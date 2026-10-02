module Page.Library exposing (Model, Msg, init, onUrlChange, onUrlRequest, subscriptions, update, view)

import Api.Post
import App.Keyboard as Keyboard exposing (KeyEvent, Modifiers, Target(..))
import App.Prefs as Prefs exposing (Prefs)
import App.Route as Route exposing (Route, View(..))
import Browser
import Browser.Dom as Dom
import Browser.Events
import Browser.Navigation as Nav
import Domain.Post exposing (PostSummary, SearchResponse)
import Domain.Query as Query
import Domain.Selection as Selection exposing (Selection)
import Domain.Sequence as Sequence exposing (Sequence)
import Feature.Compare
import Feature.Filmstrip as Filmstrip
import Feature.Inspector
import Feature.MediaGrid as MediaGrid
import Feature.MediaGrid.Layout as Layout exposing (Geometry, Viewport)
import Feature.Navigator
import Feature.QueryEditor as QueryEditor
import Feature.QuickLook as QuickLook exposing (Zoom(..))
import Feature.Shortcuts as Shortcuts
import Feature.Survey as Survey
import Html exposing (Html, button, div, footer, h1, header, p, span, text)
import Html.Attributes exposing (attribute, class, classList, id, tabindex, title, type_)
import Html.Events exposing (onClick, preventDefaultOn)
import Http
import Json.Decode as Decode
import Process
import Set exposing (Set)
import Task exposing (Task)
import Ui.Button
import Ui.Kbd
import Ui.Panel exposing (Presentation(..))
import Url exposing (Url)



-- MODEL


type Mode
    = Grid
    | Loupe
    | Compare ComparePair
    | Survey SurveySet


type alias ComparePair =
    { select : String
    , candidate : String
    }


type alias SurveySet =
    { ids : List String
    , note : Maybe String
    }


type SearchState
    = Idle
    | Searching
    | Ready
    | Failed String


type alias ReturnPoint =
    { scrollTop : Float
    , activeAtEntry : Maybe String
    }


type alias Model =
    { key : Nav.Key
    , apiBase : String
    , query : String
    , draftQuery : String
    , search : SearchState
    , requestId : Int
    , sequence : Sequence
    , selection : Selection
    , mode : Mode
    , zoom : Zoom
    , viewport : Viewport
    , returnPoint : Maybe ReturnPoint
    , windowWidth : Int
    , navigatorDrawer : Bool
    , inspectorDrawer : Bool
    , prefs : Prefs
    , missing : Set String
    , recent : List String
    , hint : Maybe String
    , shortcutsOpen : Bool
    , pendingRoute : Maybe Route
    , ownQuery : Maybe String
    , urlSeq : Int
    }


type alias Flags =
    { apiBase : String
    , prefs : Prefs
    , width : Int
    , height : Int
    }


decodeFlags : Decode.Value -> Flags
decodeFlags value =
    let
        get name decoder fallback =
            Decode.decodeValue (Decode.field name decoder) value |> Result.withDefault fallback
    in
    { apiBase = get "apiBase" Decode.string ""
    , prefs = get "prefs" Prefs.decoder Prefs.default
    , width = get "width" Decode.int 1280
    , height = get "height" Decode.int 800
    }


init : Decode.Value -> Url -> Nav.Key -> ( Model, Cmd Msg )
init flagsValue url key =
    let
        flags =
            decodeFlags flagsValue

        route =
            Route.fromUrl url

        model =
            { key = key
            , apiBase = flags.apiBase
            , query = route.query
            , draftQuery = route.query
            , search = Idle
            , requestId = 0
            , sequence = Sequence.empty
            , selection = Selection.empty
            , mode = Grid
            , zoom = Fit
            , viewport = { scrollTop = 0, width = toFloat flags.width, height = toFloat flags.height }
            , returnPoint = Nothing
            , windowWidth = flags.width
            , navigatorDrawer = False
            , inspectorDrawer = False
            , prefs = flags.prefs
            , missing = Set.empty
            , recent = []
            , hint = Nothing
            , shortcutsOpen = False
            , pendingRoute = Nothing
            , ownQuery = Nothing
            , urlSeq = 0
            }
    in
    if String.trim route.query == "" then
        ( model, measureGrid 0 )

    else
        let
            ( searching, searchCmd ) =
                startSearch { model | pendingRoute = Just route } route.query
        in
        ( searching, Cmd.batch [ searchCmd, measureGrid 0 ] )



-- UPDATE


type Command
    = MoveBy Int Bool
    | MoveRows Int Bool
    | MovePage Int Bool
    | MoveEdge Bool Bool
    | StepSelected Int
    | ToggleLoupe
    | OpenLoupe
    | OpenCompare
    | OpenSurvey
    | BackToGrid
    | Escape
    | EscapeField
    | ToggleActiveSelected
    | SelectAll
    | ClearSelection
    | ThumbStep Int
    | CycleExtras
    | ToggleZoom
    | FocusQuery
    | ToggleFilterBar
    | ToggleInspector
    | ToggleNavigator
    | ShowShortcuts
    | CloseShortcuts
    | Pending String


type Msg
    = UrlChanged Url
    | UrlRequested Browser.UrlRequest
    | DraftChanged String
    | SearchSubmitted
    | SearchCompleted Int (Result Http.Error SearchResponse)
    | Retry
    | RunQuery String
    | KeyCommand Command
    | CellClicked String Modifiers
    | CellOpened String
    | CellChecked String
    | FilmstripClicked String Modifiers
    | PaneActivated String
    | SurveyRemoved String
    | MediaFailed String
    | GridScrolled Viewport
    | GridMeasured (Result Dom.Error Dom.Viewport)
    | ReturnPointMeasured (Result Dom.Error Dom.Viewport)
    | WindowResized Int Int
    | TagClicked String Bool
    | TermRemoved Int
    | ThumbChanged Int
    | SaveSearch
    | RemoveSavedSearch String
    | SyncUrl Int
    | NoOp


onUrlChange : Url -> Msg
onUrlChange =
    UrlChanged


onUrlRequest : Browser.UrlRequest -> Msg
onUrlRequest =
    UrlRequested


{-| Keeps the address bar in step with Quick Look (debounced, `replaceUrl`)
and keeps the Filmstrip centered on the active post.
-}
update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    let
        ( next, cmd ) =
            updateHelp msg model

        urlFollows =
            case msg of
                UrlChanged _ ->
                    False

                SyncUrl _ ->
                    False

                _ ->
                    currentRoute next /= currentRoute model

        ( synced, syncCmd ) =
            if urlFollows then
                ( { next | urlSeq = next.urlSeq + 1 }
                , Process.sleep 250 |> Task.perform (\_ -> SyncUrl (next.urlSeq + 1))
                )

            else
                ( next, Cmd.none )

        filmstripCmd =
            case ( next.mode, next.selection.active ) of
                ( Grid, _ ) ->
                    Cmd.none

                ( _, Just postId ) ->
                    if next.selection.active /= model.selection.active || model.mode == Grid then
                        centerFilmstrip postId

                    else
                        Cmd.none

                ( _, Nothing ) ->
                    Cmd.none
    in
    ( synced, Cmd.batch [ cmd, syncCmd, filmstripCmd ] )


updateHelp : Msg -> Model -> ( Model, Cmd Msg )
updateHelp msg model =
    case msg of
        UrlRequested request ->
            case request of
                Browser.Internal url ->
                    ( model, Nav.pushUrl model.key (Url.toString url) )

                Browser.External href ->
                    ( model, Nav.load href )

        UrlChanged url ->
            let
                route =
                    Route.fromUrl url

                own =
                    model.ownQuery == Just route.query

                cleared =
                    { model | ownQuery = Nothing }
            in
            if route.query /= model.query || own then
                if String.trim route.query == "" then
                    ( clearResults cleared, Cmd.none )

                else
                    startSearch
                        { cleared
                            | pendingRoute =
                                if own then
                                    Nothing

                                else
                                    Just route
                        }
                        route.query

            else if route == currentRoute model then
                ( cleared, Cmd.none )

            else
                applyRoute route cleared

        DraftChanged draft ->
            ( { model | draftQuery = draft }, Cmd.none )

        SearchSubmitted ->
            runQuery model.draftQuery model

        RunQuery query ->
            runQuery query model

        TagClicked tag exclude ->
            runQuery
                (if exclude then
                    Query.excludeTerm tag model.query

                 else
                    Query.addTerm tag model.query
                )
                model

        TermRemoved index ->
            runQuery (Query.removeTerm index model.query) model

        SearchCompleted requestId result ->
            if requestId /= model.requestId then
                ( model, Cmd.none )

            else
                case result of
                    Ok response ->
                        resultsArrived response.posts model

                    Err error ->
                        ( { model | search = Failed (httpErrorToString error), pendingRoute = Nothing }, Cmd.none )

        Retry ->
            if String.trim model.query == "" then
                ( model, Cmd.none )

            else
                startSearch model model.query

        KeyCommand command ->
            runCommand command { model | hint = Nothing }

        CellClicked postId modifiers ->
            ( { model | selection = pointerSelect Selection.click postId modifiers model, hint = Nothing }, Cmd.none )

        CellOpened postId ->
            openMode Loupe { model | selection = Selection.setActive postId model.selection }

        CellChecked postId ->
            ( { model | selection = Selection.toggle postId model.selection }, Cmd.none )

        FilmstripClicked postId modifiers ->
            let
                selected =
                    { model | selection = pointerSelect Selection.setActive postId modifiers model }
            in
            case model.mode of
                Compare pair ->
                    if not modifiers.ctrl && not modifiers.shift && postId /= pair.select then
                        ( { selected | mode = Compare { pair | candidate = postId } }, Cmd.none )

                    else
                        ( selected, Cmd.none )

                _ ->
                    ( selected, Cmd.none )

        PaneActivated postId ->
            ( { model | selection = Selection.setActive postId model.selection }, Cmd.none )

        SurveyRemoved postId ->
            removeFromSurvey postId model

        MediaFailed postId ->
            ( { model | missing = Set.insert postId model.missing }, Cmd.none )

        GridScrolled viewport ->
            if model.mode == Grid then
                ( { model | viewport = viewport }, Cmd.none )

            else
                ( model, Cmd.none )

        GridMeasured result ->
            case result of
                Ok measured ->
                    ( { model
                        | viewport =
                            { scrollTop = measured.viewport.y
                            , width = measured.viewport.width
                            , height = measured.viewport.height
                            }
                      }
                    , Cmd.none
                    )

                Err _ ->
                    ( model, Cmd.none )

        ReturnPointMeasured result ->
            case ( result, model.returnPoint ) of
                ( Ok measured, Just returnPoint ) ->
                    ( { model
                        | returnPoint = Just { returnPoint | scrollTop = measured.viewport.y }
                        , viewport = setScroll measured.viewport.y model.viewport
                      }
                    , Cmd.none
                    )

                _ ->
                    ( model, Cmd.none )

        WindowResized width _ ->
            ( { model | windowWidth = width }, measureGrid 40 )

        ThumbChanged size ->
            setThumb size model

        SaveSearch ->
            let
                query =
                    String.trim model.query
            in
            if query == "" || List.member query model.prefs.savedSearches then
                ( model, Cmd.none )

            else
                savePrefs (\prefs -> { prefs | savedSearches = prefs.savedSearches ++ [ query ] }) model

        RemoveSavedSearch query ->
            savePrefs (\prefs -> { prefs | savedSearches = List.filter ((/=) query) prefs.savedSearches }) model

        SyncUrl seq ->
            if seq == model.urlSeq then
                ( model, Nav.replaceUrl model.key (Route.toHref (currentRoute model)) )

            else
                ( model, Cmd.none )

        NoOp ->
            ( model, Cmd.none )


runCommand : Command -> Model -> ( Model, Cmd Msg )
runCommand command model =
    case command of
        MoveBy delta extend ->
            case model.mode of
                Grid ->
                    moveInGrid extend (\index -> index + delta) model

                Loupe ->
                    moveInLoupe (\index -> index + delta) model

                Compare pair ->
                    moveCandidate delta pair model

                Survey survey ->
                    moveInSurvey delta survey model

        MoveRows delta extend ->
            case model.mode of
                Grid ->
                    moveInGrid extend (\index -> index + delta * (geometry model).columns) model

                Loupe ->
                    ( model, Cmd.none )

                Compare pair ->
                    if delta < 0 then
                        promoteCandidate pair model

                    else
                        ( { model | mode = Compare { select = pair.candidate, candidate = pair.select } }, Cmd.none )

                Survey survey ->
                    moveInSurvey (delta * Survey.columns (isNarrow model) (List.length survey.ids)) survey model

        MovePage delta extend ->
            if model.mode == Grid then
                let
                    g =
                        geometry model
                in
                moveInGrid extend (\index -> index + delta * g.columns * Layout.rowsPerPage g model.viewport.height) model

            else
                ( model, Cmd.none )

        MoveEdge toEnd extend ->
            let
                edge _ =
                    if toEnd then
                        Sequence.length model.sequence - 1

                    else
                        0
            in
            case model.mode of
                Grid ->
                    moveInGrid extend edge model

                Loupe ->
                    moveInLoupe edge model

                _ ->
                    ( model, Cmd.none )

        StepSelected delta ->
            case model.mode of
                Loupe ->
                    case stepSelected delta model of
                        Just postId ->
                            ( { model | selection = Selection.setActive postId model.selection }, Cmd.none )

                        Nothing ->
                            ( model, Cmd.none )

                Compare pair ->
                    moveCandidate delta pair model

                _ ->
                    ( model, Cmd.none )

        ToggleLoupe ->
            if model.mode == Loupe then
                exitToGrid model

            else
                openLoupe model

        OpenLoupe ->
            if model.mode == Loupe then
                ( model, Cmd.none )

            else
                openLoupe model

        OpenCompare ->
            openCompare model

        OpenSurvey ->
            openSurvey model

        BackToGrid ->
            if model.mode == Grid then
                ( model, Cmd.none )

            else
                exitToGrid model

        Escape ->
            if drawersOpen model then
                ( { model | navigatorDrawer = False, inspectorDrawer = False }, Cmd.none )

            else if model.mode /= Grid then
                exitToGrid model

            else
                ( model, Cmd.none )

        EscapeField ->
            ( { model | draftQuery = model.query }, Task.attempt (\_ -> NoOp) (Dom.blur QueryEditor.fieldId) )

        ToggleActiveSelected ->
            case model.selection.active of
                Just postId ->
                    case model.mode of
                        Survey _ ->
                            if Selection.isSelected postId model.selection then
                                removeFromSurvey postId model

                            else
                                ( { model | selection = Selection.toggle postId model.selection }, Cmd.none )

                        _ ->
                            ( { model | selection = Selection.toggle postId model.selection }, Cmd.none )

                Nothing ->
                    ( model, Cmd.none )

        SelectAll ->
            ( { model | selection = Selection.selectAll model.sequence model.selection }, Cmd.none )

        ClearSelection ->
            let
                cleared =
                    { model | selection = Selection.clear model.selection }
            in
            case model.mode of
                Survey _ ->
                    exitToGrid cleared

                _ ->
                    ( cleared, Cmd.none )

        ThumbStep direction ->
            if model.mode == Grid then
                setThumb (Prefs.thumbStep direction model.prefs.thumbSize) model

            else
                ( model, Cmd.none )

        CycleExtras ->
            savePrefs (\prefs -> { prefs | cellExtras = Prefs.nextExtras prefs.cellExtras }) model

        ToggleZoom ->
            case model.mode of
                Loupe ->
                    ( { model | zoom = QuickLook.toggleZoom model.zoom }, Cmd.none )

                Compare _ ->
                    ( { model | zoom = QuickLook.toggleZoom model.zoom }, Cmd.none )

                _ ->
                    ( model, Cmd.none )

        FocusQuery ->
            ( model, Task.attempt (\_ -> NoOp) (Dom.focus QueryEditor.fieldId) )

        ToggleFilterBar ->
            savePrefs (\prefs -> { prefs | filterBar = not prefs.filterBar }) model
                |> withCmd (measureGrid 40)

        ToggleInspector ->
            if inspectorPresentation model == Docked then
                savePrefs (\prefs -> { prefs | inspectorDocked = not prefs.inspectorDocked }) model
                    |> withCmd (measureGrid 40)

            else
                ( { model | inspectorDrawer = not model.inspectorDrawer, navigatorDrawer = False }, Cmd.none )

        ToggleNavigator ->
            if navigatorPresentation model == Docked then
                savePrefs (\prefs -> { prefs | navigatorDocked = not prefs.navigatorDocked }) model
                    |> withCmd (measureGrid 40)

            else
                ( { model | navigatorDrawer = not model.navigatorDrawer, inspectorDrawer = False }, Cmd.none )

        ShowShortcuts ->
            ( { model | shortcutsOpen = True }, focusLater Shortcuts.elementId )

        CloseShortcuts ->
            ( { model | shortcutsOpen = False }, Cmd.none )

        Pending feature ->
            ( { model | hint = Just (feature ++ " needs a server API that does not exist yet.") }, Cmd.none )



-- SEARCH


runQuery : String -> Model -> ( Model, Cmd Msg )
runQuery rawQuery model =
    let
        query =
            String.trim rawQuery

        route =
            currentRoute model
    in
    ( { model | ownQuery = Just query, draftQuery = query }
    , Nav.pushUrl model.key (Route.toHref { route | query = query })
    )


startSearch : Model -> String -> ( Model, Cmd Msg )
startSearch model query =
    let
        requestId =
            model.requestId + 1
    in
    ( { model | query = query, draftQuery = query, search = Searching, requestId = requestId }
    , Api.Post.search model.apiBase query (SearchCompleted requestId)
    )


clearResults : Model -> Model
clearResults model =
    { model
        | query = ""
        , draftQuery = ""
        , search = Idle
        , requestId = model.requestId + 1
        , sequence = Sequence.empty
        , selection = Selection.empty
        , mode = Grid
        , returnPoint = Nothing
        , viewport = setScroll 0 model.viewport
        , pendingRoute = Nothing
    }


{-| New results keep the workspace: panels, thumbnail size, and mode stay; the
active post and selection survive when their posts are still present.
-}
resultsArrived : List PostSummary -> Model -> ( Model, Cmd Msg )
resultsArrived posts model =
    let
        sequence =
            Sequence.fromList posts

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode

        base =
            { model
                | sequence = sequence
                , selection = selection
                , mode = mode
                , search = Ready
                , recent = remember model.query model.recent
                , pendingRoute = Nothing
            }

        top =
            case activeIndex base of
                Just index ->
                    Layout.revealTop (geometry base) (setScroll 0 base.viewport) index

                Nothing ->
                    0

        scrolled =
            { base
                | viewport = setScroll top base.viewport
                , returnPoint =
                    if mode == Grid then
                        Nothing

                    else
                        Just { scrollTop = top, activeAtEntry = selection.active }
            }

        ( routed, routeCmd ) =
            case model.pendingRoute of
                Just route ->
                    applyRoute route scrolled

                Nothing ->
                    ( scrolled, Cmd.none )
    in
    ( routed, Cmd.batch [ scrollGridLater top, routeCmd ] )


validMode : Sequence -> Selection -> Mode -> Mode
validMode sequence selection mode =
    case mode of
        Grid ->
            Grid

        Loupe ->
            if selection.active == Nothing then
                Grid

            else
                Loupe

        Compare pair ->
            if Sequence.member pair.select sequence && Sequence.member pair.candidate sequence then
                mode

            else
                Grid

        Survey survey ->
            let
                kept =
                    List.filter (\postId -> Sequence.member postId sequence) survey.ids
            in
            if List.length kept >= 2 then
                Survey { survey | ids = kept }

            else
                Grid


applyRoute : Route -> Model -> ( Model, Cmd Msg )
applyRoute route model =
    case ( route.view, route.post ) of
        ( LoupeView, Just postId ) ->
            if Sequence.member postId model.sequence then
                let
                    activated =
                        { model | selection = Selection.setActive postId model.selection }
                in
                if model.mode == Loupe then
                    ( activated, Cmd.none )

                else
                    let
                        top =
                            activeIndex activated
                                |> Maybe.map (Layout.revealTop (geometry activated) activated.viewport)
                                |> Maybe.withDefault activated.viewport.scrollTop
                    in
                    enterMode Loupe { activated | viewport = setScroll top activated.viewport }
                        |> withCmd (scrollGridLater top)

            else
                ( model, Cmd.none )

        _ ->
            if model.mode == Loupe then
                exitToGrid model

            else
                ( model, Cmd.none )


currentRoute : Model -> Route
currentRoute model =
    case ( model.mode, model.selection.active ) of
        ( Loupe, Just postId ) ->
            { query = model.query, post = Just postId, view = LoupeView }

        _ ->
            { query = model.query, post = Nothing, view = GridView }


remember : String -> List String -> List String
remember query recent =
    if String.trim query == "" then
        recent

    else
        query :: List.filter ((/=) query) recent |> List.take 8



-- MODES


enterMode : Mode -> Model -> ( Model, Cmd Msg )
enterMode mode model =
    let
        returnPoint =
            case model.mode of
                Grid ->
                    Just { scrollTop = model.viewport.scrollTop, activeAtEntry = model.selection.active }

                _ ->
                    model.returnPoint
    in
    ( { model | mode = mode, returnPoint = returnPoint }, focusLater stageViewId )


{-| A scroll event can still be in flight when the user opens a mode, so the
return point takes the browser's offset rather than the model's.
-}
openMode : Mode -> Model -> ( Model, Cmd Msg )
openMode mode model =
    enterMode mode model
        |> withCmd
            (if model.mode == Grid then
                Task.attempt ReturnPointMeasured (Dom.getViewportOf MediaGrid.elementId)

             else
                Cmd.none
            )


{-| Back to the grid at the exact saved scroll offset, unless the active post
moved out of that view, in which case scroll the minimum distance to reveal it.
-}
exitToGrid : Model -> ( Model, Cmd Msg )
exitToGrid model =
    let
        returnPoint =
            Maybe.withDefault { scrollTop = model.viewport.scrollTop, activeAtEntry = Nothing } model.returnPoint

        g =
            geometry model

        saved =
            setScroll returnPoint.scrollTop model.viewport

        top =
            case activeIndex model of
                Just index ->
                    if model.selection.active == returnPoint.activeAtEntry || Layout.isFullyVisible g saved index then
                        returnPoint.scrollTop

                    else
                        Layout.revealTop g saved index

                Nothing ->
                    returnPoint.scrollTop
    in
    ( { model | mode = Grid, returnPoint = Nothing, zoom = Fit, viewport = setScroll top model.viewport }
    , Cmd.batch
        [ Task.attempt (\_ -> NoOp) (Dom.setViewportOf MediaGrid.elementId 0 top)
        , case model.selection.active of
            Just postId ->
                focusLater (MediaGrid.cellId postId)

            Nothing ->
                Cmd.none
        ]
    )


openLoupe : Model -> ( Model, Cmd Msg )
openLoupe model =
    case ensureActive model of
        Just withActive ->
            openMode Loupe withActive

        Nothing ->
            ( model, Cmd.none )


openCompare : Model -> ( Model, Cmd Msg )
openCompare model =
    case ensureActive model |> Maybe.andThen (\withActive -> compareFor withActive |> Maybe.map (Tuple.pair withActive)) of
        Just ( withActive, pair ) ->
            openMode (Compare pair) { withActive | selection = Selection.setActive pair.select withActive.selection }

        Nothing ->
            ( { model | hint = Just "Compare needs at least two posts." }, Cmd.none )


openSurvey : Model -> ( Model, Cmd Msg )
openSurvey model =
    let
        ordered =
            Sequence.inOrder model.selection.selected model.sequence

        count =
            List.length ordered
    in
    if count < 2 then
        ( { model | hint = Just "Select 2–6 images to survey." }, Cmd.none )

    else
        let
            start =
                if count <= 6 then
                    0

                else
                    model.selection.active
                        |> Maybe.andThen (\postId -> indexIn postId ordered)
                        |> Maybe.map (\index -> min index (count - 6))
                        |> Maybe.withDefault 0

            ids =
                ordered |> List.drop start |> List.take 6

            note =
                if count > 6 then
                    Just ("Showing 6 of " ++ String.fromInt count ++ " selected")

                else
                    Nothing

            selection =
                case ( model.selection.active, ids ) of
                    ( Just postId, first :: _ ) ->
                        if List.member postId ids then
                            model.selection

                        else
                            Selection.setActive first model.selection

                    ( Nothing, first :: _ ) ->
                        Selection.setActive first model.selection

                    _ ->
                        model.selection
        in
        openMode (Survey { ids = ids, note = note }) { model | selection = selection }


compareFor : Model -> Maybe ComparePair
compareFor model =
    let
        ordered =
            Sequence.inOrder model.selection.selected model.sequence

        active =
            model.selection.active
    in
    case ordered of
        [ a, b ] ->
            if active == Just b then
                Just { select = b, candidate = a }

            else
                Just { select = a, candidate = b }

        a :: _ :: _ ->
            let
                select =
                    case active of
                        Just postId ->
                            if List.member postId ordered then
                                postId

                            else
                                a

                        Nothing ->
                            a
            in
            cycle 1 select (List.filter ((/=) select) ordered |> (::) select)
                |> Maybe.map (\candidate -> { select = select, candidate = candidate })

        _ ->
            active
                |> Maybe.andThen
                    (\postId ->
                        case stepSkipping 1 postId postId model.sequence of
                            Just candidate ->
                                Just { select = postId, candidate = candidate }

                            Nothing ->
                                stepSkipping -1 postId postId model.sequence
                                    |> Maybe.map (\candidate -> { select = postId, candidate = candidate })
                    )


moveCandidate : Int -> ComparePair -> Model -> ( Model, Cmd Msg )
moveCandidate delta pair model =
    case nextCandidate delta pair model of
        Just candidate ->
            ( { model
                | mode = Compare { pair | candidate = candidate }
                , selection = Selection.setActive candidate model.selection
              }
            , Cmd.none
            )

        Nothing ->
            ( model, Cmd.none )


promoteCandidate : ComparePair -> Model -> ( Model, Cmd Msg )
promoteCandidate pair model =
    let
        promoted =
            { select = pair.candidate, candidate = pair.select }

        candidate =
            nextCandidate 1 promoted model |> Maybe.withDefault pair.select
    in
    ( { model
        | mode = Compare { promoted | candidate = candidate }
        , selection = Selection.setActive candidate model.selection
      }
    , Cmd.none
    )


{-| With more than two selected, candidates cycle through the selection;
otherwise they walk the result sequence, skipping the select.
-}
nextCandidate : Int -> ComparePair -> Model -> Maybe String
nextCandidate delta pair model =
    let
        ordered =
            Sequence.inOrder model.selection.selected model.sequence
    in
    if List.length ordered > 2 then
        cycle delta pair.candidate (List.filter ((/=) pair.select) ordered)

    else
        stepSkipping delta pair.select pair.candidate model.sequence


moveInSurvey : Int -> SurveySet -> Model -> ( Model, Cmd Msg )
moveInSurvey delta survey model =
    let
        current =
            model.selection.active |> Maybe.andThen (\postId -> indexIn postId survey.ids) |> Maybe.withDefault 0

        target =
            clamp 0 (List.length survey.ids - 1) (current + delta)
    in
    case survey.ids |> List.drop target |> List.head of
        Just postId ->
            ( { model | selection = Selection.setActive postId model.selection }, Cmd.none )

        Nothing ->
            ( model, Cmd.none )


removeFromSurvey : String -> Model -> ( Model, Cmd Msg )
removeFromSurvey postId model =
    case model.mode of
        Survey survey ->
            let
                remaining =
                    List.filter ((/=) postId) survey.ids

                deselected =
                    Selection.remove postId model.selection

                selection =
                    if model.selection.active == Just postId then
                        case nextAfter postId survey.ids of
                            Just next ->
                                Selection.setActive next deselected

                            Nothing ->
                                deselected

                    else
                        deselected

                updated =
                    { model | selection = selection }
            in
            if List.length remaining < 2 then
                exitToGrid updated

            else
                ( { updated | mode = Survey { survey | ids = remaining } }, Cmd.none )

        _ ->
            ( { model | selection = Selection.remove postId model.selection }, Cmd.none )



-- MOVEMENT


moveInGrid : Bool -> (Int -> Int) -> Model -> ( Model, Cmd Msg )
moveInGrid extend toIndex model =
    let
        count =
            Sequence.length model.sequence
    in
    if count == 0 then
        ( model, Cmd.none )

    else
        case activeIndex model of
            Nothing ->
                selectIndex False (clamp 0 (count - 1) (Layout.firstVisibleIndex (geometry model) model.viewport.scrollTop)) model

            Just current ->
                selectIndex extend (clamp 0 (count - 1) (toIndex current)) model


selectIndex : Bool -> Int -> Model -> ( Model, Cmd Msg )
selectIndex extend index model =
    case Sequence.idAt index model.sequence of
        Just postId ->
            revealActive
                { model
                    | selection =
                        if extend then
                            Selection.extendTo model.sequence postId model.selection

                        else
                            Selection.setActive postId model.selection
                }

        Nothing ->
            ( model, Cmd.none )


revealActive : Model -> ( Model, Cmd Msg )
revealActive model =
    case ( model.selection.active, activeIndex model ) of
        ( Just postId, Just index ) ->
            let
                top =
                    Layout.revealTop (geometry model) model.viewport index

                scroll =
                    if top /= model.viewport.scrollTop then
                        Dom.setViewportOf MediaGrid.elementId 0 top

                    else
                        Task.succeed ()
            in
            ( { model | viewport = setScroll top model.viewport }
            , scroll
                |> Task.andThen (\_ -> focusWithRetry (MediaGrid.cellId postId))
                |> Task.attempt (\_ -> NoOp)
            )

        _ ->
            ( model, Cmd.none )


moveInLoupe : (Int -> Int) -> Model -> ( Model, Cmd Msg )
moveInLoupe toIndex model =
    case activeIndex model of
        Just current ->
            case Sequence.idAt (clamp 0 (Sequence.length model.sequence - 1) (toIndex current)) model.sequence of
                Just postId ->
                    ( { model | selection = Selection.setActive postId model.selection }, Cmd.none )

                Nothing ->
                    ( model, Cmd.none )

        Nothing ->
            ( model, Cmd.none )


stepSelected : Int -> Model -> Maybe String
stepSelected delta model =
    let
        current =
            activeIndex model |> Maybe.withDefault -1

        indexed =
            Sequence.inOrder model.selection.selected model.sequence
                |> List.filterMap (\postId -> Sequence.indexOf postId model.sequence |> Maybe.map (\index -> ( index, postId )))

        candidates =
            if delta > 0 then
                List.filter (\( index, _ ) -> index > current) indexed

            else
                List.filter (\( index, _ ) -> index < current) indexed |> List.reverse
    in
    List.head candidates |> Maybe.map Tuple.second


pointerSelect : (String -> Selection -> Selection) -> String -> Modifiers -> Model -> Selection
pointerSelect plain postId modifiers model =
    if modifiers.ctrl && modifiers.shift then
        Selection.addRange model.sequence postId model.selection

    else if modifiers.shift then
        Selection.selectRange model.sequence postId model.selection

    else if modifiers.ctrl then
        Selection.toggle postId model.selection

    else
        plain postId model.selection


setThumb : Int -> Model -> ( Model, Cmd Msg )
setThumb requested model =
    let
        size =
            clamp Prefs.thumbMin Prefs.thumbMax requested
    in
    if size == model.prefs.thumbSize then
        ( model, Cmd.none )

    else
        let
            oldGeometry =
                geometry model

            anchor =
                case activeIndex model of
                    Just index ->
                        if Layout.isFullyVisible oldGeometry model.viewport index then
                            index

                        else
                            Layout.firstVisibleIndex oldGeometry model.viewport.scrollTop

                    Nothing ->
                        Layout.firstVisibleIndex oldGeometry model.viewport.scrollTop

            offset =
                Layout.cellTop oldGeometry anchor - model.viewport.scrollTop

            ( resized, prefsCmd ) =
                savePrefs (\prefs -> { prefs | thumbSize = size }) model

            top =
                clampScroll resized (Layout.cellTop (geometry resized) anchor - offset)
        in
        ( { resized | viewport = setScroll top resized.viewport }, Cmd.batch [ prefsCmd, scrollGridLater top ] )



-- HELPERS


geometry : Model -> Geometry
geometry model =
    Layout.geometry model.viewport.width model.prefs.thumbSize


activeIndex : Model -> Maybe Int
activeIndex model =
    model.selection.active |> Maybe.andThen (\postId -> Sequence.indexOf postId model.sequence)


activePost : Model -> Maybe PostSummary
activePost model =
    model.selection.active |> Maybe.andThen (\postId -> Sequence.find postId model.sequence)


ensureActive : Model -> Maybe Model
ensureActive model =
    case model.selection.active of
        Just _ ->
            Just model

        Nothing ->
            Sequence.idAt (Layout.firstVisibleIndex (geometry model) model.viewport.scrollTop) model.sequence
                |> Maybe.map (\postId -> { model | selection = Selection.setActive postId model.selection })


setScroll : Float -> Viewport -> Viewport
setScroll top viewport =
    { viewport | scrollTop = top }


clampScroll : Model -> Float -> Float
clampScroll model top =
    clamp 0 (max 0 (Layout.totalHeight (geometry model) (Sequence.length model.sequence) - model.viewport.height)) top


savePrefs : (Prefs -> Prefs) -> Model -> ( Model, Cmd Msg )
savePrefs change model =
    let
        prefs =
            change model.prefs
    in
    ( { model | prefs = prefs }, Prefs.save prefs )


withCmd : Cmd Msg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
withCmd extra ( model, cmd ) =
    ( model, Cmd.batch [ cmd, extra ] )


indexIn : String -> List String -> Maybe Int
indexIn postId ids =
    ids
        |> List.indexedMap Tuple.pair
        |> List.filter (\( _, candidate ) -> candidate == postId)
        |> List.head
        |> Maybe.map Tuple.first


nextAfter : String -> List String -> Maybe String
nextAfter postId ids =
    case indexIn postId ids of
        Just index ->
            case List.drop (index + 1) ids |> List.head of
                Just next ->
                    Just next

                Nothing ->
                    List.drop (index - 1) ids |> List.head

        Nothing ->
            Nothing


cycle : Int -> String -> List String -> Maybe String
cycle delta current ids =
    let
        count =
            List.length ids
    in
    if count == 0 then
        Nothing

    else
        case indexIn current ids of
            Just index ->
                List.drop (modBy count (index + delta)) ids |> List.head

            Nothing ->
                List.head ids


stepSkipping : Int -> String -> String -> Sequence -> Maybe String
stepSkipping delta skip from sequence =
    let
        walk index =
            case Sequence.idAt index sequence of
                Just postId ->
                    if postId == skip then
                        walk (index + delta)

                    else
                        Just postId

                Nothing ->
                    Nothing
    in
    Sequence.indexOf from sequence |> Maybe.andThen (\index -> walk (index + delta))


isNarrow : Model -> Bool
isNarrow model =
    model.windowWidth < 760


inspectorPresentation : Model -> Presentation
inspectorPresentation model =
    if model.windowWidth >= 1100 then
        Docked

    else if model.windowWidth >= 760 then
        Drawer

    else
        Sheet


inspectorVisible : Model -> Bool
inspectorVisible model =
    if inspectorPresentation model == Docked then
        model.prefs.inspectorDocked

    else
        model.inspectorDrawer


navigatorPresentation : Model -> Presentation
navigatorPresentation model =
    if model.windowWidth >= 1440 then
        Docked

    else
        Drawer


navigatorVisible : Model -> Bool
navigatorVisible model =
    if navigatorPresentation model == Docked then
        model.prefs.navigatorDocked

    else
        model.navigatorDrawer


drawersOpen : Model -> Bool
drawersOpen model =
    (navigatorPresentation model /= Docked && model.navigatorDrawer)
        || (inspectorPresentation model /= Docked && model.inspectorDrawer)


stageViewId : String
stageViewId =
    "stage-view"


measureGrid : Float -> Cmd Msg
measureGrid delay =
    Process.sleep delay
        |> Task.andThen (\_ -> Dom.getViewportOf MediaGrid.elementId)
        |> Task.attempt GridMeasured


{-| Waits for the next render so the spacer height is current, then scrolls
and re-measures so the model matches what the browser actually did.
-}
scrollGridLater : Float -> Cmd Msg
scrollGridLater top =
    Process.sleep 30
        |> Task.andThen (\_ -> Dom.setViewportOf MediaGrid.elementId 0 top)
        |> Task.andThen (\_ -> Dom.getViewportOf MediaGrid.elementId)
        |> Task.attempt GridMeasured


focusLater : String -> Cmd Msg
focusLater elementId =
    Process.sleep 40
        |> Task.andThen (\_ -> focusWithRetry elementId)
        |> Task.attempt (\_ -> NoOp)


focusWithRetry : String -> Task Dom.Error ()
focusWithRetry elementId =
    Dom.focus elementId
        |> Task.onError (\_ -> Process.sleep 60 |> Task.andThen (\_ -> Dom.focus elementId))


centerFilmstrip : String -> Cmd Msg
centerFilmstrip postId =
    Process.sleep 40
        |> Task.andThen
            (\_ ->
                Task.map3
                    (\strip box item ->
                        strip.viewport.x + (item.element.x - box.element.x) - (box.element.width - item.element.width) / 2
                    )
                    (Dom.getViewportOf Filmstrip.elementId)
                    (Dom.getElement Filmstrip.elementId)
                    (Dom.getElement (Filmstrip.itemId postId))
            )
        |> Task.andThen (\x -> Dom.setViewportOf Filmstrip.elementId (max 0 x) 0)
        |> Task.attempt (\_ -> NoOp)


httpErrorToString : Http.Error -> String
httpErrorToString error =
    case error of
        Http.BadUrl url ->
            "Invalid API URL: " ++ url

        Http.Timeout ->
            "The search timed out."

        Http.NetworkError ->
            "The search server could not be reached."

        Http.BadStatus status ->
            "Search failed (HTTP " ++ String.fromInt status ++ ")."

        Http.BadBody _ ->
            "The search response was invalid."



-- KEYBOARD


commandFor : Model -> KeyEvent -> Maybe Command
commandFor model event =
    if model.shortcutsOpen then
        if event.key == "Escape" || event.key == "?" then
            Just CloseShortcuts

        else
            Nothing

    else if event.target == Editable then
        if event.key == "Escape" then
            Just EscapeField

        else
            Nothing

    else if event.target == Control && (event.key == " " || event.key == "Enter") then
        Nothing

    else if event.ctrl then
        case String.toLower event.key of
            "a" ->
                Just SelectAll

            "d" ->
                Just ClearSelection

            _ ->
                Nothing

    else if event.alt then
        case event.key of
            "ArrowLeft" ->
                Just (StepSelected -1)

            "ArrowRight" ->
                Just (StepSelected 1)

            _ ->
                Nothing

    else
        case event.key of
            "ArrowLeft" ->
                Just (MoveBy -1 event.shift)

            "ArrowRight" ->
                Just (MoveBy 1 event.shift)

            "ArrowUp" ->
                Just (MoveRows -1 event.shift)

            "ArrowDown" ->
                Just (MoveRows 1 event.shift)

            "PageUp" ->
                Just (MovePage -1 event.shift)

            "PageDown" ->
                Just (MovePage 1 event.shift)

            "Home" ->
                Just (MoveEdge False event.shift)

            "End" ->
                Just (MoveEdge True event.shift)

            " " ->
                Just ToggleLoupe

            "Enter" ->
                Just OpenLoupe

            "Escape" ->
                Just Escape

            "?" ->
                Just ShowShortcuts

            "/" ->
                Just FocusQuery

            "\\" ->
                Just ToggleFilterBar

            "-" ->
                Just (ThumbStep -1)

            "_" ->
                Just (ThumbStep -1)

            "=" ->
                Just (ThumbStep 1)

            "+" ->
                Just (ThumbStep 1)

            _ ->
                case String.toLower event.key of
                    "g" ->
                        Just BackToGrid

                    "e" ->
                        Just OpenLoupe

                    "c" ->
                        Just OpenCompare

                    "n" ->
                        if event.shift then
                            Just ToggleNavigator

                        else
                            Just OpenSurvey

                    "s" ->
                        Just ToggleActiveSelected

                    "z" ->
                        Just ToggleZoom

                    "j" ->
                        Just CycleExtras

                    "i" ->
                        Just ToggleInspector

                    "t" ->
                        Just (Pending "Tag editing")

                    "b" ->
                        Just (Pending "Collections")

                    "f" ->
                        Just (Pending "Favorites")

                    _ ->
                        Nothing


keyDecoder : Model -> Decode.Decoder ( Msg, Bool )
keyDecoder model =
    Keyboard.decoder
        |> Decode.andThen
            (\event ->
                case commandFor model event of
                    Just command ->
                        Decode.succeed ( KeyCommand command, True )

                    Nothing ->
                        Decode.fail "unhandled key"
            )


subscriptions : Model -> Sub Msg
subscriptions model =
    Sub.batch
        [ Browser.Events.onResize WindowResized
        , Browser.Events.onKeyDown
            (Keyboard.bodyDecoder
                |> Decode.andThen
                    (\event ->
                        case commandFor model event of
                            Just command ->
                                Decode.succeed (KeyCommand command)

                            Nothing ->
                                Decode.fail "unhandled key"
                    )
            )
        ]



-- VIEW


view : Model -> Browser.Document Msg
view model =
    { title = documentTitle model
    , body = [ shell model ]
    }


documentTitle : Model -> String
documentTitle model =
    if String.trim model.query == "" then
        "Kura"

    else
        case ( model.mode, model.selection.active ) of
            ( Loupe, Just postId ) ->
                "#" ++ postId ++ " · " ++ model.query ++ " · Kura"

            _ ->
                model.query ++ " · Kura"


shell : Model -> Html Msg
shell model =
    div [ class "kura-shell", preventDefaultOn "keydown" (keyDecoder model) ]
        [ topBar model
        , div [ class "progress", classList [ ( "is-active", model.search == Searching ) ] ] []
        , div [ class "kura-body" ]
            [ navigatorView model
            , div [ class "workspace" ]
                [ if model.prefs.filterBar then
                    QueryEditor.filterBar
                        { query = model.query
                        , thumb = model.prefs.thumbSize
                        , extras = model.prefs.cellExtras
                        , onRemoveTerm = TermRemoved
                        , onThumb = ThumbChanged
                        , onCycleExtras = KeyCommand CycleExtras
                        }

                  else
                    text ""
                , stage model
                ]
            , inspectorView model
            ]
        , statusBar model
        , if model.shortcutsOpen then
            Shortcuts.view (KeyCommand CloseShortcuts)

          else
            text ""
        ]


topBar : Model -> Html Msg
topBar model =
    header [ class "topbar" ]
        [ Ui.Button.view [ class "button-quiet topbar-toggle" ]
            { label = "Library"
            , key = Nothing
            , onPress = Just (KeyCommand ToggleNavigator)
            , pressed = Just (navigatorVisible model)
            , hint = Just "Toggle navigator (Shift+N)"
            }
        , h1 [ class "wordmark" ] [ text "Kura" ]
        , QueryEditor.field { draft = model.draftQuery, onInput = DraftChanged, onSubmit = SearchSubmitted }
        , span [ class "topbar-count" ] [ text (countLabel model) ]
        , div [ class "mode-switch", attribute "role" "group", attribute "aria-label" "View mode" ]
            [ modeButton model "Grid" "G" BackToGrid (model.mode == Grid)
            , modeButton model "Loupe" "E" OpenLoupe (model.mode == Loupe)
            , modeButton model "Compare" "C" OpenCompare (isCompare model.mode)
            , modeButton model "Survey" "N" OpenSurvey (isSurvey model.mode)
            ]
        , Ui.Button.view [ class "button-quiet" ]
            { label = "Info"
            , key = Just "I"
            , onPress = Just (KeyCommand ToggleInspector)
            , pressed = Just (inspectorVisible model)
            , hint = Just "Toggle inspector"
            }
        ]


modeButton : Model -> String -> String -> Command -> Bool -> Html Msg
modeButton model label key command active =
    Ui.Button.view [ class "mode-button" ]
        { label = label
        , key = Just key
        , onPress =
            if Sequence.isEmpty model.sequence then
                Nothing

            else
                Just (KeyCommand command)
        , pressed = Just active
        , hint = Nothing
        }


isCompare : Mode -> Bool
isCompare mode =
    case mode of
        Compare _ ->
            True

        _ ->
            False


isSurvey : Mode -> Bool
isSurvey mode =
    case mode of
        Survey _ ->
            True

        _ ->
            False


countLabel : Model -> String
countLabel model =
    case model.search of
        Searching ->
            "Searching…"

        _ ->
            if String.trim model.query == "" then
                ""

            else
                let
                    count =
                        Sequence.length model.sequence
                in
                String.fromInt count
                    ++ (if count == 1 then
                            " post"

                        else
                            " posts"
                       )


navigatorView : Model -> Html Msg
navigatorView model =
    if navigatorVisible model then
        Feature.Navigator.view
            { presentation = navigatorPresentation model
            , current = model.query
            , saved = model.prefs.savedSearches
            , recent = model.recent
            , onRun = RunQuery
            , onSave = SaveSearch
            , onRemove = RemoveSavedSearch
            , onClose = KeyCommand ToggleNavigator
            }

    else
        text ""


inspectorView : Model -> Html Msg
inspectorView model =
    if inspectorVisible model then
        Feature.Inspector.view
            { apiBase = model.apiBase
            , presentation = inspectorPresentation model
            , active = activePost model
            , selectedCount = Selection.count model.selection
            , commonTags = commonTags model
            , missing = model.missing
            , onTag = TagClicked
            , onClear = KeyCommand ClearSelection
            , onClose = KeyCommand ToggleInspector
            , onApiPending = KeyCommand << Pending
            , onMediaError = MediaFailed
            }

    else
        text ""


commonTags : Model -> List String
commonTags model =
    case Sequence.inOrder model.selection.selected model.sequence |> List.filterMap (\postId -> Sequence.find postId model.sequence) of
        first :: rest ->
            List.filter (\tag -> List.all (\post -> List.member tag post.tags) rest) first.tags

        [] ->
            []


stage : Model -> Html Msg
stage model =
    div [ class "stage" ]
        [ MediaGrid.view
            { apiBase = model.apiBase
            , sequence = model.sequence
            , selection = model.selection
            , thumb = model.prefs.thumbSize
            , extras = model.prefs.cellExtras
            , viewport = model.viewport
            , missing = model.missing
            , hidden = model.mode /= Grid
            , stale = model.search == Searching && not (Sequence.isEmpty model.sequence)
            , onCell = CellClicked
            , onOpen = CellOpened
            , onCheck = CellChecked
            , onScroll = GridScrolled
            , onMediaError = MediaFailed
            , noop = NoOp
            }
        , if model.mode == Grid then
            text ""

          else
            div [ id stageViewId, class "stage-view", tabindex -1 ]
                [ modeView model
                , Filmstrip.view
                    { apiBase = model.apiBase
                    , sequence = model.sequence
                    , selection = model.selection
                    , missing = model.missing
                    , onClick = FilmstripClicked
                    , onMediaError = MediaFailed
                    }
                ]
        , emptyState model
        ]


modeView : Model -> Html Msg
modeView model =
    case model.mode of
        Grid ->
            text ""

        Loupe ->
            case ( activePost model, activeIndex model ) of
                ( Just post, Just index ) ->
                    QuickLook.view
                        { apiBase = model.apiBase
                        , post = post
                        , index = index
                        , total = Sequence.length model.sequence
                        , zoom = model.zoom
                        , missing = Set.member post.id model.missing
                        , onZoom = KeyCommand ToggleZoom
                        , onPrevious = KeyCommand (MoveBy -1 False)
                        , onNext = KeyCommand (MoveBy 1 False)
                        , onClose = KeyCommand BackToGrid
                        , onMediaError = MediaFailed
                        }

                _ ->
                    text ""

        Compare pair ->
            case ( Sequence.find pair.select model.sequence, Sequence.find pair.candidate model.sequence ) of
                ( Just select, Just candidate ) ->
                    Feature.Compare.view
                        { apiBase = model.apiBase
                        , select = select
                        , candidate = candidate
                        , activeId = model.selection.active
                        , zoom = model.zoom
                        , missing = model.missing
                        , onPrevious = KeyCommand (MoveBy -1 False)
                        , onNext = KeyCommand (MoveBy 1 False)
                        , onSwap = KeyCommand (MoveRows 1 False)
                        , onPromote = KeyCommand (MoveRows -1 False)
                        , onZoom = KeyCommand ToggleZoom
                        , onClose = KeyCommand BackToGrid
                        , onActivate = PaneActivated
                        , onMediaError = MediaFailed
                        }

                _ ->
                    text ""

        Survey survey ->
            Survey.view
                { apiBase = model.apiBase
                , posts = List.filterMap (\postId -> Sequence.find postId model.sequence) survey.ids
                , activeId = model.selection.active
                , missing = model.missing
                , narrow = isNarrow model
                , note = survey.note
                , onActivate = PaneActivated
                , onRemove = SurveyRemoved
                , onClose = KeyCommand BackToGrid
                , onMediaError = MediaFailed
                }


emptyState : Model -> Html Msg
emptyState model =
    if not (Sequence.isEmpty model.sequence) then
        text ""

    else
        div [ class "stage-empty" ]
            (case model.search of
                Idle ->
                    [ p [] [ text "Enter a search to browse media." ]
                    , button [ class "button", type_ "button", onClick (RunQuery "demo") ] [ text "Try “demo”" ]
                    ]

                Searching ->
                    [ p [] [ text "Searching…" ] ]

                Ready ->
                    [ p [] [ text "No posts found." ] ]

                Failed message ->
                    [ p [ class "status-error" ] [ text message ]
                    , button [ class "button", type_ "button", onClick Retry ] [ text "Retry" ]
                    ]
            )


statusBar : Model -> Html Msg
statusBar model =
    let
        selectedCount =
            Selection.count model.selection
    in
    footer [ class "statusbar" ]
        [ span [ class "status-item" ]
            [ text
                (case model.selection.active of
                    Just postId ->
                        "Active #" ++ postId

                    Nothing ->
                        "No active post"
                )
            ]
        , span [ class "status-item" ] [ text (String.fromInt selectedCount ++ " selected") ]
        , span [ class "status-item" ] [ text (String.fromInt (Sequence.length model.sequence) ++ " loaded") ]
        , case model.search of
            Failed message ->
                span [ class "status-item status-error" ]
                    [ text message
                    , button [ class "button button-quiet", type_ "button", onClick Retry ] [ text "Retry" ]
                    ]

            _ ->
                text ""
        , span [ class "status-item status-hint", attribute "role" "status" ]
            [ text (Maybe.withDefault "" model.hint) ]
        , button [ class "button button-quiet statusbar-help", type_ "button", onClick (KeyCommand ShowShortcuts), title "Keyboard shortcuts" ]
            [ span [] [ text "Shortcuts" ], Ui.Kbd.view "?" ]
        ]
