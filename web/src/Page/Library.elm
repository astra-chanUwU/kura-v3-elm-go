module Page.Library exposing (Model, Msg, init, onUrlChange, onUrlRequest, subscriptions, update, view)

import Api.Post
import Api.Access
import Api.Collection
import Api.SavedSearch
import App.Access
import App.Keyboard as Keyboard exposing (KeyEvent, Modifiers, Target(..))
import App.Prefs as Prefs exposing (Prefs)
import App.Route as Route exposing (Route, View(..))
import Browser
import Browser.Dom as Dom
import Browser.Events
import Browser.Navigation as Nav
import Domain.Post exposing (PostDetail, PostSummary, SearchResponse, TagEditResponse, TagEditTarget, ReactionResponse, ReactionTarget, summaryOfDetail)
import Domain.Collection exposing (Collection)
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
import Feature.UploadQueue as UploadQueue
import Html exposing (Html, button, div, footer, form, h1, header, input, p, span, text)
import Html.Attributes exposing (attribute, class, classList, disabled, id, placeholder, tabindex, title, type_, value)
import Html.Events exposing (onClick, onInput, onSubmit, preventDefaultOn)
import Http
import Json.Decode as Decode
import Dict exposing (Dict)
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
    | LoadingMore
    | Ready
    | Failed String


type alias ReturnPoint =
    { scrollTop : Float
    , activeAtEntry : Maybe String
    }


type SearchRequest
    = InitialRequest
    | MoreRequest


type DetailState
    = DetailLoading
    | DetailReady PostDetail
    | DetailFailed String


type alias Model =
    { key : Nav.Key
    , apiBase : String
    , query : String
    , draftQuery : String
    , search : SearchState
    , requestId : Int
    , nextCursor : Maybe String
    , details : Dict String DetailState
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
    , tagAddDraft : String
    , tagRemoveDraft : String
    , tagSaving : Bool
    , tagStatus : Maybe String
    , tagRequestId : Int
    , tagVersions : Dict String Int
    , revertConfirm : Maybe Int
    , revertPending : Maybe Int
    , revertStatus : Maybe String
    , reactionVersions : Dict String Int
    , reactionSaving : Bool
    , reactionStatus : Maybe String
    , reactionRequestId : Int
    , scoreDraft : String
    , collections : List Collection
    , activeCollection : Maybe String
    , collectionBrowse : Maybe String
    , collectionDraft : String
    , collectionStatus : Maybe String
    , collectionRemovals : Set String
    , savedSearches : List Api.SavedSearch.SavedSearch
    , savedSearchFallback : Bool
    , savedSearchSaving : Bool
    , savedSearchStatus : Maybe String
    , upload : UploadQueue.Model
    , uploadedPosts : Dict String PostSummary
    , uploadOrigin : Maybe UploadOrigin
    , access : App.Access.State
    }


{-| The result set an upload View navigated away from. Restored when the
user returns to the grid so View never corrupts the originating search
or collection.
-}
type alias UploadOrigin =
    { sequence : Sequence
    , selection : Selection
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
            , nextCursor = Nothing
            , details = Dict.empty
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
            , tagAddDraft = ""
            , tagRemoveDraft = ""
            , tagSaving = False
            , tagStatus = Nothing
            , tagRequestId = 0
            , tagVersions = Dict.empty
            , revertConfirm = Nothing
            , revertPending = Nothing
            , revertStatus = Nothing
            , reactionVersions = Dict.empty
            , reactionSaving = False
            , reactionStatus = Nothing
            , reactionRequestId = 0
            , scoreDraft = "0"
            , collections = []
            , activeCollection = Nothing
            , collectionBrowse = Nothing
            , collectionDraft = ""
            , collectionStatus = Nothing
            , collectionRemovals = Set.empty
            , savedSearches = []
            , savedSearchFallback = True
            , savedSearchSaving = False
            , savedSearchStatus = Nothing
            , upload = UploadQueue.init
            , uploadedPosts = Dict.empty
            , uploadOrigin = Nothing
            , access = App.Access.init
            }
    in
    let
        ( searching, searchCmd ) =
            startSearch { model | pendingRoute = Just route } route.query
    in
    -- Collections and search are public reads. Saved searches are
    -- owner-scoped, so their list waits for gate discovery; the local
    -- fallback shows until then.
    ( searching
    , Cmd.batch
        [ searchCmd
        , measureGrid 0
        , Api.Collection.list flags.apiBase CollectionsCompleted
        , Api.Access.discover flags.apiBase (CapabilitiesReceived model.access.generation)
        ]
    )



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
    | ToggleFavoriteAction
    | ToggleNavigator
    | ShowShortcuts
    | CloseShortcuts
    | Pending String


type Msg
    = UrlChanged Url
    | UrlRequested Browser.UrlRequest
    | DraftChanged String
    | SearchSubmitted
    | SearchCompleted Int SearchRequest (Result Http.Error SearchResponse)
    | DetailCompleted Int String (Result Http.Error PostDetail)
    | TagAddChanged String
    | TagRemoveChanged String
    | SaveTags
    | RevertTags Int
    | ConfirmRevert Int
    | CancelRevert
    | TagsCompleted Int Int (List String) (Result Http.Error TagEditResponse)
    | ToggleFavorite
    | ScoreDraftChanged String
    | SaveScore
    | ReactionsCompleted Int Int (List String) (Result Http.Error ReactionResponse)
    | CollectionsCompleted (Result Http.Error (List Collection))
    | CapabilitiesReceived Int (Result Http.Error Api.Access.Capabilities)
    | TokenDraftChanged String
    | UnlockRequested
    | LockRequested
    | AccessRetry
    | SavedSearchesCompleted Int (Result Http.Error (List Api.SavedSearch.SavedSearch))
    | SavedSearchCreated Int (Result Http.Error Api.SavedSearch.SavedSearch)
    | SavedSearchDeleted Int String (Result Http.Error ())
    | CollectionDraftChanged String
    | CreateCollection String
    | CollectionCreated Int (Result Http.Error Collection)
    | SelectCollection String
    | OpenCollection String
    | OpenAllPosts
    | CollectionPostsCompleted Int String (Result Http.Error SearchResponse)
    | AddToCollection
    | CollectionAdded Int (Result Http.Error Collection)
    | MoveCollectionPost String Int
    | CollectionReordered Int (Result Http.Error Collection)
    | RemoveCollectionPost String
    | CollectionPostRemoved Int String (Result Http.Error ())
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
    | UploadMsg UploadQueue.Msg
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
        ( updated, cmd ) =
            updateHelp msg model

        ( next, detailCmd ) =
            if updated.selection.active /= model.selection.active then
                prepareActiveDetail updated

            else
                ( updated, Cmd.none )

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
    ( synced, Cmd.batch [ cmd, syncCmd, filmstripCmd, detailCmd ] )


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

        OpenAllPosts ->
            runQuery "" model

        OpenCollection collectionId ->
            openCollection model collectionId

        CollectionPostsCompleted requestId collectionId result ->
            if requestId /= model.requestId then
                ( model, Cmd.none )

            else
                case result of
                    Ok response ->
                        collectionResultsArrived collectionId response model

                    Err error ->
                        ( { model | search = Failed (httpErrorToString error), collectionBrowse = Just collectionId }, Cmd.none )

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

        SearchCompleted requestId requestKind result ->
            if requestId /= model.requestId then
                ( model, Cmd.none )

            else
                case ( requestKind, result ) of
                    ( InitialRequest, Ok response ) ->
                        resultsArrived response model

                    ( MoreRequest, Ok response ) ->
                        moreResultsArrived response model

                    ( _, Err error ) ->
                        ( { model | search = Failed (httpErrorToString error), pendingRoute = Nothing }, Cmd.none )

        DetailCompleted generation postId result ->
            if not (App.Access.isCurrent generation model.access) then
                ( model, Cmd.none )

            else
                ( { model
                    | sequence =
                        case result of
                            Ok detail ->
                                Sequence.refreshSummary (summaryOfDetail detail) model.sequence

                            Err _ ->
                                model.sequence
                    , details =
                        Dict.insert postId
                            (case result of
                                Ok detail ->
                                    DetailReady detail

                                Err error ->
                                    DetailFailed (httpErrorToString error)
                            )
                            model.details
                    , tagVersions =
                        case result of
                            Ok detail ->
                                Dict.insert postId detail.tagVersion model.tagVersions

                            Err _ ->
                                model.tagVersions
                    , reactionVersions =
                        case result of
                            Ok detail ->
                                Dict.insert postId detail.reactionVersion model.reactionVersions

                            Err _ ->
                                model.reactionVersions
                    , scoreDraft =
                        if model.selection.active == Just postId then
                            case result of
                                Ok detail ->
                                    String.fromInt detail.score

                                Err _ ->
                                    model.scoreDraft

                        else
                            model.scoreDraft
                  }
                , Cmd.none
                )

        TagAddChanged value ->
            ( { model | tagAddDraft = value, tagStatus = Nothing }, Cmd.none )

        TagRemoveChanged value ->
            ( { model | tagRemoveDraft = value, tagStatus = Nothing }, Cmd.none )

        SaveTags ->
            saveTags model

        RevertTags targetVersion ->
            requestRevert model targetVersion

        ConfirmRevert targetVersion ->
            confirmRevert model targetVersion

        CancelRevert ->
            ( { model | revertConfirm = Nothing, revertStatus = Nothing }, Cmd.none )

        CapabilitiesReceived generation result ->
            if not (App.Access.isCurrent generation model.access) then
                ( model, Cmd.none )

            else
                capabilitiesSettled { model | access = App.Access.discovered generation result model.access }

        TokenDraftChanged draft ->
            ( { model | access = App.Access.draftChanged draft model.access }, Cmd.none )

        UnlockRequested ->
            let
                ( access, candidate ) =
                    App.Access.unlockRequested model.access
            in
            case candidate of
                Nothing ->
                    ( { model | access = access }, Cmd.none )

                Just token ->
                    ( { model | access = access }
                    , Api.Access.validate model.apiBase token (CapabilitiesReceived access.generation)
                    )

        LockRequested ->
            lockNow model "Locked. Browsing stays available."

        AccessRetry ->
            let
                access =
                    App.Access.retryRequested model.access
            in
            ( { model | access = access }
            , Api.Access.discover model.apiBase (CapabilitiesReceived access.generation)
            )

        TagsCompleted generation requestId postIds result ->
            if not (App.Access.isCurrent generation model.access) then
                refreshPublicPosts postIds model

            else if requestId /= model.tagRequestId then
                ( model, Cmd.none )

            else
                case result of
                    Ok response ->
                        let
                            sequence =
                                List.foldl
                                    (\post sequence_ -> Sequence.updateTags post.id post.tags sequence_)
                                    model.sequence
                                    response.posts

                            versions =
                                List.foldl (\post versions_ -> Dict.insert post.id post.version versions_) model.tagVersions response.posts

                            wasRevert =
                                model.revertPending /= Nothing

                            pendingVersion =
                                model.revertPending

                            refreshed =
                                case model.selection.active of
                                    Just postId ->
                                        ( { model
                                            | sequence = sequence
                                            , tagVersions = versions
                                            , tagSaving = False
                                            , revertPending = Nothing
                                            , revertConfirm = Nothing
                                            , tagStatus =
                                                if wasRevert then
                                                    Nothing

                                                else
                                                    Just "Tags saved."
                                            , revertStatus =
                                                case pendingVersion of
                                                    Just v ->
                                                        Just ("Reverted to v" ++ String.fromInt v ++ ".")

                                                    Nothing ->
                                                        Nothing
                                            , tagAddDraft = ""
                                            , tagRemoveDraft = ""
                                            , details = Dict.insert postId DetailLoading model.details
                                          }
                                        , Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation postId)
                                        )

                                    Nothing ->
                                        ( { model
                                            | sequence = sequence
                                            , tagVersions = versions
                                            , tagSaving = False
                                            , revertPending = Nothing
                                            , revertConfirm = Nothing
                                            , tagStatus =
                                                if wasRevert then
                                                    Nothing

                                                else
                                                    Just "Tags saved."
                                            , revertStatus =
                                                case pendingVersion of
                                                    Just v ->
                                                        Just ("Reverted to v" ++ String.fromInt v ++ ".")

                                                    Nothing ->
                                                        Nothing
                                            , tagAddDraft = ""
                                            , tagRemoveDraft = ""
                                          }
                                        , Cmd.none
                                        )
                        in
                        refreshed

                    Err (Http.BadStatus 401) ->
                        revokeAccess model

                    Err error ->
                        let
                            wasRevert =
                                model.revertPending /= Nothing

                            message =
                                tagErrorToString error
                        in
                        ( { model
                            | tagSaving = False
                            , revertPending = Nothing
                            , tagStatus =
                                if wasRevert then
                                    Nothing

                                else
                                    Just message
                            , revertStatus =
                                if wasRevert then
                                    Just message

                                else
                                    Nothing
                          }
                        , Cmd.none
                        )

        ToggleFavorite ->
            saveFavorite model

        ScoreDraftChanged value ->
            ( { model | scoreDraft = value, reactionStatus = Nothing }, Cmd.none )

        SaveScore ->
            saveScore model

        ReactionsCompleted generation requestId postIds result ->
            if not (App.Access.isCurrent generation model.access) then
                refreshPublicPosts postIds model

            else if requestId /= model.reactionRequestId then
                ( model, Cmd.none )

            else
                case result of
                    Ok response ->
                        let
                            versions =
                                List.foldl (\post versions_ -> Dict.insert post.id post.version versions_) model.reactionVersions response.posts

                            refreshed =
                                case model.selection.active of
                                    Just postId ->
                                        ( { model
                                            | reactionVersions = versions
                                            , reactionSaving = False
                                            , reactionStatus = Just "Rating saved."
                                            , details = Dict.insert postId DetailLoading model.details
                                          }
                                        , Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation postId)
                                        )

                                    Nothing ->
                                        ( { model | reactionVersions = versions, reactionSaving = False, reactionStatus = Just "Rating saved." }, Cmd.none )
                        in
                        refreshed

                    Err (Http.BadStatus 401) ->
                        revokeAccess model

                    Err error ->
                        ( { model | reactionSaving = False, reactionStatus = Just (reactionErrorToString error) }, Cmd.none )

        CollectionsCompleted result ->
            case result of
                Ok collections ->
                    ( { model
                        | collections = collections
                        , activeCollection =
                            case model.activeCollection of
                                Just id ->
                                    if List.any (\collection -> collection.id == id) collections then
                                        Just id

                                    else
                                        List.head collections |> Maybe.map .id

                                Nothing ->
                                    List.head collections |> Maybe.map .id
                      }
                    , Cmd.none
                    )

                Err error ->
                    ( { model | collectionStatus = Just (httpErrorToString error) }, Cmd.none )

        SavedSearchesCompleted generation result ->
            if not (App.Access.isCurrent generation model.access) then
                ( model, Cmd.none )

            else
                case result of
                    Ok savedSearches ->
                        ( { model
                            | savedSearches = savedSearches
                            , savedSearchFallback = False
                            , savedSearchStatus = Nothing
                          }
                        , Cmd.none
                        )

                    Err (Http.BadStatus 401) ->
                        if App.Access.canWrite model.access then
                            revokeAccess model

                        else
                            ( { model
                                | savedSearchFallback = True
                                , savedSearchStatus = Nothing
                              }
                            , Cmd.none
                            )

                    Err _ ->
                        ( { model
                            | savedSearchFallback = True
                            , savedSearchStatus = Nothing
                          }
                        , Cmd.none
                        )

        SavedSearchCreated generation result ->
            if not (App.Access.isCurrent generation model.access) then
                refreshCurrentOwnerSearches model

            else
                case result of
                    Ok savedSearch ->
                        ( { model
                            | savedSearches = model.savedSearches ++ [ savedSearch ]
                            , savedSearchSaving = False
                            , savedSearchStatus = Just "Saved search created."
                          }
                        , Cmd.none
                        )

                    Err (Http.BadStatus 401) ->
                        if App.Access.canWrite model.access then
                            revokeAccess model

                        else
                            fallbackSaveSearch model

                    Err _ ->
                        if model.savedSearchFallback then
                            fallbackSaveSearch model

                        else
                            ( { model
                                | savedSearchSaving = False
                                , savedSearchStatus = Just "Could not save the search."
                              }
                            , Cmd.none
                            )

        SavedSearchDeleted generation id result ->
            if not (App.Access.isCurrent generation model.access) then
                refreshCurrentOwnerSearches model

            else
                case result of
                    Ok () ->
                        ( { model
                            | savedSearches = List.filter (\savedSearch -> savedSearch.id /= id) model.savedSearches
                            , savedSearchSaving = False
                            , savedSearchStatus = Just "Saved search removed."
                          }
                        , Cmd.none
                        )

                    Err (Http.BadStatus 401) ->
                        if App.Access.canWrite model.access then
                            revokeAccess model

                        else
                            ( { model
                                | savedSearchSaving = False
                                , savedSearchStatus = Just "Could not remove saved search."
                              }
                            , Cmd.none
                            )

                    Err _ ->
                        ( { model
                            | savedSearchSaving = False
                            , savedSearchStatus = Just "Could not remove saved search."
                          }
                        , Cmd.none
                        )

        CollectionDraftChanged value ->
            ( { model | collectionDraft = value, collectionStatus = Nothing }, Cmd.none )

        CreateCollection name ->
            if String.trim name == "" then
                ( model, Cmd.none )

            else if not (App.Access.canWrite model.access) then
                ( { model | collectionStatus = Just "Unlock to create collections." }, Cmd.none )

            else
                ( { model | collectionStatus = Just "Creating collection…" }
                , Api.Collection.create model.apiBase (App.Access.credential model.access) name (CollectionCreated model.access.generation)
                )

        CollectionCreated generation result ->
            if not (App.Access.isCurrent generation model.access) then
                ( model, Api.Collection.list model.apiBase CollectionsCompleted )

            else
                case result of
                    Ok collection ->
                        ( { model
                            | collections = model.collections ++ [ collection ]
                            , activeCollection = Just collection.id
                            , collectionDraft = ""
                            , collectionStatus = Just "Collection created."
                          }
                        , Cmd.none
                        )

                    Err (Http.BadStatus 401) ->
                        revokeAccess model

                    Err error ->
                        ( { model | collectionStatus = Just (httpErrorToString error) }, Cmd.none )

        SelectCollection id ->
            ( { model | activeCollection = Just id, collectionStatus = Nothing }, Cmd.none )

        AddToCollection ->
            if not (App.Access.canWrite model.access) then
                ( { model | collectionStatus = Just "Unlock to add to a collection." }, Cmd.none )

            else
                case model.activeCollection of
                    Just collectionId ->
                        let
                            postIds = Selection.targets model.sequence model.selection
                        in
                        if List.isEmpty postIds then
                            ( { model | collectionStatus = Just "Select a post first." }, Cmd.none )

                        else
                            ( { model | collectionStatus = Just "Adding posts…" }
                            , Api.Collection.addPosts model.apiBase (App.Access.credential model.access) collectionId postIds (CollectionAdded model.access.generation)
                            )

                    Nothing ->
                        ( { model | collectionStatus = Just "Create or select a collection first." }, Cmd.none )

        CollectionAdded generation result ->
            if not (App.Access.isCurrent generation model.access) then
                ( model, Api.Collection.list model.apiBase CollectionsCompleted )

            else
                case result of
                    Ok updated ->
                        ( { model
                            | collections = List.map (\collection -> if collection.id == updated.id then updated else collection) model.collections
                            , collectionStatus = Just "Posts added to collection."
                          }
                        , Cmd.none
                        )

                    Err (Http.BadStatus 401) ->
                        revokeAccess model

                    Err error ->
                        ( { model | collectionStatus = Just (httpErrorToString error) }, Cmd.none )

        MoveCollectionPost postId delta ->
            if not (App.Access.canWrite model.access) then
                ( { model | collectionStatus = Just "Unlock to reorder collections." }, Cmd.none )

            else
                case model.activeCollection of
                    Nothing ->
                        ( { model | collectionStatus = Just "Select a collection first." }, Cmd.none )

                    Just collectionId ->
                        case List.filter (\c -> c.id == collectionId) model.collections |> List.head of
                            Nothing ->
                                ( { model | collectionStatus = Just "Select a collection first." }, Cmd.none )

                            Just collection ->
                                let
                                    next =
                                        moveInList postId delta collection.postIds
                                in
                                case next of
                                    Nothing ->
                                        ( model, Cmd.none )

                                    Just reordered ->
                                        ( { model | collectionStatus = Just "Reordering…" }
                                        , Api.Collection.reorder model.apiBase (App.Access.credential model.access) collectionId reordered (CollectionReordered model.access.generation)
                                        )

        CollectionReordered generation result ->
            if not (App.Access.isCurrent generation model.access) then
                ( model, Api.Collection.list model.apiBase CollectionsCompleted )

            else
                case result of
                    Ok updated ->
                        ( { model
                            | collections = List.map (\c -> if c.id == updated.id then updated else c) model.collections
                            , collectionStatus = Just "Order saved."
                          }
                        , Cmd.none
                        )

                    Err (Http.BadStatus 401) ->
                        revokeAccess model

                    Err error ->
                        ( { model | collectionStatus = Just (httpErrorToString error) }, Cmd.none )

        RemoveCollectionPost postId ->
            if not (App.Access.canWrite model.access) then
                ( { model | collectionStatus = Just "Unlock to remove from collections." }, Cmd.none )

            else
                case model.activeCollection of
                    Nothing ->
                        ( { model | collectionStatus = Just "Select a collection first." }, Cmd.none )

                    Just collectionId ->
                        if Set.member postId model.collectionRemovals then
                            ( model, Cmd.none )

                        else
                            case List.filter (\c -> c.id == collectionId) model.collections |> List.head of
                                Nothing ->
                                    ( { model | collectionStatus = Just "Select a collection first." }, Cmd.none )

                                Just collection ->
                                    if not (List.member postId collection.postIds) then
                                        ( model, Cmd.none )

                                    else
                                        ( { model | collectionStatus = Just "Removing…", collectionRemovals = Set.insert postId model.collectionRemovals }
                                        , Api.Collection.removePost model.apiBase (App.Access.credential model.access) collectionId postId (CollectionPostRemoved model.access.generation postId)
                                        )

        CollectionPostRemoved generation postId result ->
            if not (App.Access.isCurrent generation model.access) then
                ( model, Api.Collection.list model.apiBase CollectionsCompleted )

            else
                case result of
                    Ok () ->
                        let
                            updatedCollections =
                                List.map
                                    (\collection ->
                                        if Just collection.id == model.activeCollection then
                                            { collection | postIds = List.filter ((/=) postId) collection.postIds }

                                        else
                                            collection
                                    )
                                    model.collections
                        in
                        ( { model | collections = updatedCollections, collectionRemovals = Set.remove postId model.collectionRemovals, collectionStatus = Just "Removed from collection." }, Cmd.none )

                    Err (Http.BadStatus 401) ->
                        revokeAccess model

                    Err error ->
                        ( { model | collectionRemovals = Set.remove postId model.collectionRemovals, collectionStatus = Just (httpErrorToString error) }, Cmd.none )

        Retry ->
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
                let
                    updated =
                        { model | viewport = viewport }
                in
                if shouldLoadMore updated then
                    loadMore updated

                else
                    ( updated, Cmd.none )

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
            if query == "" || List.any (\savedSearch -> savedSearch.label == query) (savedItems model) then
                ( model, Cmd.none )

            else if model.savedSearchFallback then
                savePrefs
                    (\prefs -> { prefs | savedSearches = prefs.savedSearches ++ [ query ] })
                    { model | savedSearchStatus = Just "Saved search saved on this device." }

            else if not (App.Access.canWrite model.access) then
                fallbackSaveSearch model

            else
                ( { model | savedSearchSaving = True, savedSearchStatus = Just "Saving saved search…" }
                , Api.SavedSearch.create model.apiBase (App.Access.credential model.access) query query (SavedSearchCreated model.access.generation)
                )

        RemoveSavedSearch id ->
            if model.savedSearchFallback then
                fallbackRemoveSavedSearch id model

            else if not (App.Access.canWrite model.access) then
                ( { model
                    | savedSearchSaving = False
                    , savedSearchStatus = Just "Unlock to remove saved searches."
                  }
                , Cmd.none
                )

            else
                ( { model | savedSearchSaving = True, savedSearchStatus = Just "Removing saved search…" }
                , Api.SavedSearch.delete model.apiBase (App.Access.credential model.access) id (SavedSearchDeleted model.access.generation id)
                )

        SyncUrl seq ->
            if seq == model.urlSeq then
                ( model, Nav.replaceUrl model.key (Route.toHref (currentRoute model)) )

            else
                ( model, Cmd.none )

        UploadMsg subMsg ->
            case subMsg of
                UploadQueue.BrowseClicked ->
                    if App.Access.canWrite model.access then
                        delegateUpload subMsg model

                    else
                        ( { model | hint = Just "Unlock to upload images." }, Cmd.none )

                _ ->
                    delegateUpload subMsg model

        NoOp ->
            ( model, Cmd.none )


{-| Per-call queue context: uploads carry the current credential and only
start while writes are enabled. The queue never stores the credential.
-}
uploadContext : Model -> UploadQueue.Context
uploadContext model =
    UploadQueue.context model.apiBase (App.Access.credential model.access) (App.Access.canWrite model.access) model.access.generation


delegateUpload : UploadQueue.Msg -> Model -> ( Model, Cmd Msg )
delegateUpload subMsg model =
    let
        ( queue, queueCmd, outs ) =
            UploadQueue.update (uploadContext model) subMsg model.upload

        withQueue =
            { model | upload = queue }

        focusCmd =
            case subMsg of
                UploadQueue.OpenPanel ->
                    focusLater "upload-tags"

                _ ->
                    Cmd.none
    in
    handleUploadOuts outs ( withQueue, Cmd.batch [ Cmd.map UploadMsg queueCmd, focusCmd ] )


{-| Upload queue effects keep the Library stable: the query, collection,
selection, and scroll position are never touched here. Confirmed uploads
stay in the queue and detail caches; sequence members are refreshed in
place, but new posts are never injected into filtered searches or
collection results.
-}
handleUploadOuts : List UploadQueue.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleUploadOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleUploadOuts rest (applyUploadOut out ( model, cmd ))


applyUploadOut : UploadQueue.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyUploadOut out ( model, cmd ) =
    case out of
        UploadQueue.UploadConfirmed detail ->
            ( { model
                | sequence = Sequence.refreshSummary (summaryOfDetail detail) model.sequence
                , details = Dict.insert detail.id (DetailReady detail) model.details
                , uploadedPosts = Dict.insert detail.id (summaryOfDetail detail) model.uploadedPosts
                , tagVersions = Dict.insert detail.id detail.tagVersion model.tagVersions
                , reactionVersions = Dict.insert detail.id detail.reactionVersion model.reactionVersions
              }
            , cmd
            )

        UploadQueue.ThumbCompleted postId ->
            ( model
            , Cmd.batch [ cmd, Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation postId) ]
            )

        UploadQueue.OpenPost postId ->
            case Dict.get postId model.uploadedPosts of
                Just summary ->
                    openUploadPost postId summary ( model, cmd )

                Nothing ->
                    ( { model | hint = Just "That upload is no longer available." }, cmd )

        UploadQueue.CredentialRejected requestGeneration ->
            if App.Access.isCurrent requestGeneration model.access && App.Access.canWrite model.access then
                revokeAccess model

            else
                -- Already locked (for example a lock landed mid-upload):
                -- the entry already settled honestly, so there is nothing
                -- further to revoke.
                ( model, cmd )


{-| View navigates to the uploaded post on a transient single-post
sequence, saving the originating result set first. Returning to the
grid restores the origin untouched.
-}
openUploadPost : String -> PostSummary -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
openUploadPost postId summary ( model, cmd ) =
    let
        origin =
            case model.uploadOrigin of
                Just saved ->
                    Just saved

                Nothing ->
                    Just { sequence = model.sequence, selection = model.selection }
    in
    openMode Loupe
        { model
            | sequence = Sequence.fromList [ summary ]
            , selection = Selection.setActive postId Selection.empty
            , uploadOrigin = origin
        }
        |> withCmd cmd


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
            if UploadQueue.isOpen model.upload then
                ( { model | upload = UploadQueue.setOpen False model.upload }, Cmd.none )

            else if drawersOpen model then
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

        ToggleFavoriteAction ->
            saveFavorite model

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
    ( { model | query = query, draftQuery = query, search = Searching, requestId = requestId, nextCursor = Nothing, collectionBrowse = Nothing }
    , Api.Post.search model.apiBase query Nothing 60 (SearchCompleted requestId InitialRequest)
    )


openCollection : Model -> String -> ( Model, Cmd Msg )
openCollection model collectionId =
    let
        requestId =
            model.requestId + 1
    in
    ( { model
        | requestId = requestId
        , search = Searching
        , nextCursor = Nothing
        , collectionBrowse = Just collectionId
      }
    , Api.Collection.posts model.apiBase collectionId 60 (CollectionPostsCompleted requestId collectionId)
    )


clearResults : Model -> Model
clearResults model =
    { model
        | query = ""
        , draftQuery = ""
        , search = Idle
        , requestId = model.requestId + 1
        , nextCursor = Nothing
        , details = Dict.empty
        , sequence = Sequence.empty
        , selection = Selection.empty
        , mode = Grid
        , returnPoint = Nothing
        , viewport = setScroll 0 model.viewport
        , pendingRoute = Nothing
        , collectionBrowse = Nothing
    }


{-| New results keep the workspace: panels, thumbnail size, and mode stay; the
active post and selection survive when their posts are still present.
-}
resultsArrived : SearchResponse -> Model -> ( Model, Cmd Msg )
resultsArrived response model =
    let
        posts =
            response.posts

        sequence =
            Sequence.fromList posts

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode

        base =
            { model
                | sequence = sequence
                , nextCursor = response.nextCursor
                , collectionBrowse = Nothing
                , selection = selection
                , mode = mode
                , search = Ready
                , recent = remember model.query model.recent
                , pendingRoute = Nothing
                , uploadOrigin = Nothing
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


collectionResultsArrived : String -> SearchResponse -> Model -> ( Model, Cmd Msg )
collectionResultsArrived collectionId response model =
    let
        sequence =
            Sequence.fromList response.posts

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode

        updated =
            { model
                | sequence = sequence
                , nextCursor = Nothing
                , selection = selection
                , mode = mode
                , search = Ready
                , collectionBrowse = Just collectionId
                , pendingRoute = Nothing
                , viewport = setScroll 0 model.viewport
                , uploadOrigin = Nothing
            }
    in
    ( updated, scrollGridLater 0 )


moreResultsArrived : SearchResponse -> Model -> ( Model, Cmd Msg )
moreResultsArrived response model =
    let
        sequence =
            Sequence.append response.posts model.sequence

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode
    in
    ( { model
        | sequence = sequence
        , nextCursor = response.nextCursor
        , collectionBrowse = Nothing
        , selection = selection
        , mode = mode
        , search = Ready
      }
    , Cmd.none
    )


shouldLoadMore : Model -> Bool
shouldLoadMore model =
    case ( model.mode, model.search, model.nextCursor ) of
        ( Grid, Ready, Just _ ) ->
            let
                g =
                    geometry model

                contentBottom =
                    Layout.totalHeight g (Sequence.length model.sequence)

                threshold =
                    max 240 model.viewport.height
            in
            model.viewport.scrollTop + model.viewport.height >= contentBottom - threshold

        _ ->
            False


loadMore : Model -> ( Model, Cmd Msg )
loadMore model =
    case model.nextCursor of
        Just cursor ->
            ( { model | search = LoadingMore }
            , Api.Post.search model.apiBase model.query (Just cursor) 60 (SearchCompleted model.requestId MoreRequest)
            )

        Nothing ->
            ( model, Cmd.none )


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


moveInList : String -> Int -> List String -> Maybe (List String)
moveInList postId delta ids =
    case indexIn postId ids of
        Nothing ->
            Nothing

        Just idx ->
            let
                target =
                    idx + delta
            in
            if target < 0 || target >= List.length ids then
                Nothing

            else
                let
                    without =
                        List.filter ((/=) postId) ids

                    before =
                        List.take target without

                    after =
                        List.drop target without
                in
                Just (before ++ [ postId ] ++ after)


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
An upload View origin is restored first so the originating result set comes
back untouched.
-}
exitToGrid : Model -> ( Model, Cmd Msg )
exitToGrid model =
    let
        restored =
            case model.uploadOrigin of
                Just origin ->
                    { model | sequence = origin.sequence, selection = origin.selection, uploadOrigin = Nothing }

                Nothing ->
                    model

        returnPoint =
            Maybe.withDefault { scrollTop = restored.viewport.scrollTop, activeAtEntry = Nothing } restored.returnPoint

        g =
            geometry restored

        saved =
            setScroll returnPoint.scrollTop restored.viewport

        top =
            case activeIndex restored of
                Just index ->
                    if restored.selection.active == returnPoint.activeAtEntry || Layout.isFullyVisible g saved index then
                        returnPoint.scrollTop

                    else
                        Layout.revealTop g saved index

                Nothing ->
                    returnPoint.scrollTop
    in
    ( { restored | mode = Grid, returnPoint = Nothing, zoom = Fit, viewport = setScroll top restored.viewport }
    , Cmd.batch
        [ Task.attempt (\_ -> NoOp) (Dom.setViewportOf MediaGrid.elementId 0 top)
        , case restored.selection.active of
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


activeDetail : Model -> Maybe PostDetail
activeDetail model =
    model.selection.active
        |> Maybe.andThen (\postId -> Dict.get postId model.details)
        |> Maybe.andThen
            (\state ->
                case state of
                    DetailReady detail ->
                        Just detail

                    _ ->
                        Nothing
            )


activeDetailLoading : Model -> Bool
activeDetailLoading model =
    model.selection.active
        |> Maybe.andThen (\postId -> Dict.get postId model.details)
        |> Maybe.map (\state -> state == DetailLoading)
        |> Maybe.withDefault False


activeDetailError : Model -> Maybe String
activeDetailError model =
    model.selection.active
        |> Maybe.andThen (\postId -> Dict.get postId model.details)
        |> Maybe.andThen
            (\state ->
                case state of
                    DetailFailed message ->
                        Just message

                    _ ->
                        Nothing
            )


prepareActiveDetail : Model -> ( Model, Cmd Msg )
prepareActiveDetail model =
    case model.selection.active of
        Nothing ->
            ( model, Cmd.none )

        Just postId ->
            case Dict.get postId model.details of
                Just (DetailLoading) ->
                    ( model, Cmd.none )

                Just (DetailReady _) ->
                    ( model, Cmd.none )

                _ ->
                    ( { model | details = Dict.insert postId DetailLoading model.details }
                    , Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation postId)
                    )


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


{-| Settle a capabilities response into follow-up work. A fresh unlock
loads the authenticated owner's saved searches and resumes the paused
upload queue; a fresh open-local discovery loads the local actor's
list. Lock and unavailable answers clear the server list so only the
on-device fallback shows.
-}
capabilitiesSettled : Model -> ( Model, Cmd Msg )
capabilitiesSettled model =
    case model.access.status of
        App.Access.Unlocked ->
            let
                ( queue, queueCmd, outs ) =
                    UploadQueue.resume (uploadContext model) model.upload

                ( withQueue, queueCmds ) =
                    handleUploadOuts outs ( { model | upload = queue }, Cmd.map UploadMsg queueCmd )
            in
            ( { withQueue | hint = Just "Unlocked. Writes enabled." }
            , Cmd.batch [ queueCmds, loadServerSearches model ]
            )

        App.Access.OpenLocal ->
            ( model, loadServerSearches model )

        App.Access.Locked ->
            ( { model | savedSearches = [], savedSearchFallback = True }, Cmd.none )

        App.Access.Unavailable ->
            ( { model | savedSearches = [], savedSearchFallback = True }, Cmd.none )

        _ ->
            ( model, Cmd.none )


{-| Owner-scoped saved-search list for the current credential: the
system owner's entries while unlocked, the local actor's while
open-local. Never called while locked.
-}
loadServerSearches : Model -> Cmd Msg
loadServerSearches model =
    Api.SavedSearch.list model.apiBase
        (App.Access.credential model.access)
        (SavedSearchesCompleted model.access.generation)


{-| User-initiated lock: secrets clear, in-flight credentialed responses
go stale, the server list clears back to the local fallback, and public
state re-fetches so already-accepted writes still show. Lock is not
rollback and never replays mutations or uploads.
-}
lockNow : Model -> String -> ( Model, Cmd Msg )
lockNow model hint =
    reconcilePublic
        { model
            | access = App.Access.lockRequested model.access
            , savedSearches = []
            , savedSearchFallback = True
            , savedSearchSaving = False
            , tagSaving = False
            , revertPending = Nothing
            , revertConfirm = Nothing
            , reactionSaving = False
            , collectionStatus = Nothing
            , collectionRemovals = Set.empty
            , hint = Just hint
        }


{-| Server-initiated lock after a 401 on a credentialed request. Same
clearing as a manual lock, plus the fixed notice, then public
reconciliation. Stale in-flight flags reset because their completions
will arrive with an obsolete generation and be ignored.
-}
revokeAccess : Model -> ( Model, Cmd Msg )
revokeAccess model =
    reconcilePublic
        { model
            | access = App.Access.revoked model.access
            , savedSearches = []
            , savedSearchFallback = True
            , savedSearchSaving = False
            , tagSaving = False
            , revertPending = Nothing
            , revertConfirm = Nothing
            , reactionSaving = False
            , collectionStatus = Nothing
            , collectionRemovals = Set.empty
            , hint = Just "The browser was locked (401). Unlock to continue writing."
        }


{-| Re-fetch public metadata without replacing the result sequence or
resetting the workspace. Inactive details are invalidated and load lazily
on selection; current targets are bounded to one page. Late completions
also refresh their captured targets, after the server has actually settled.
-}
reconcilePublic : Model -> ( Model, Cmd Msg )
reconcilePublic model =
    let
        postIds =
            ((model.selection.active |> Maybe.map List.singleton |> Maybe.withDefault []) ++ Set.toList model.selection.selected)
                |> List.take 60

        ( refreshed, postCmd ) =
            refreshPublicPosts postIds { model | details = Dict.empty }
    in
    ( refreshed, Cmd.batch [ postCmd, Api.Collection.list model.apiBase CollectionsCompleted ] )


refreshPublicPosts : List String -> Model -> ( Model, Cmd Msg )
refreshPublicPosts postIds model =
    let
        ids =
            Set.fromList postIds |> Set.toList
    in
    ( { model | details = List.foldl (\id details -> Dict.insert id DetailLoading details) model.details ids }
    , ids |> List.map (\id -> Api.Post.detail model.apiBase id (DetailCompleted model.access.generation id)) |> Cmd.batch
    )


refreshCurrentOwnerSearches : Model -> ( Model, Cmd Msg )
refreshCurrentOwnerSearches model =
    if App.Access.canWrite model.access then
        ( model, loadServerSearches model )

    else
        ( model, Cmd.none )


fallbackSaveSearch : Model -> ( Model, Cmd Msg )
fallbackSaveSearch model =
    let
        query =
            String.trim model.query
    in
    savePrefs
        (\prefs -> { prefs | savedSearches = prefs.savedSearches ++ [ query ] })
        { model
            | savedSearchFallback = True
            , savedSearchSaving = False
            , savedSearchStatus = Just "Saved search saved on this device."
        }


fallbackRemoveSavedSearch : String -> Model -> ( Model, Cmd Msg )
fallbackRemoveSavedSearch id model =
    let
        query =
            model.savedSearches
                |> List.filter (\savedSearch -> savedSearch.id == id)
                |> List.head
                |> Maybe.map .query
                |> Maybe.withDefault id
    in
    savePrefs
        (\prefs -> { prefs | savedSearches = List.filter ((/=) query) prefs.savedSearches })
        { model
            | savedSearchFallback = True
            , savedSearchSaving = False
            , savedSearchStatus = Just "Saved search removed from this device."
        }


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


tagErrorToString : Http.Error -> String
tagErrorToString error =
    case error of
        Http.BadStatus 409 ->
            "Tags changed elsewhere. Reload the post and try again."

        Http.BadStatus 404 ->
            "One selected post no longer exists."

        Http.BadStatus status ->
            "Tag edit failed (HTTP " ++ String.fromInt status ++ ")."

        Http.BadUrl url ->
            "Invalid API URL: " ++ url

        Http.Timeout ->
            "The tag edit timed out."

        Http.NetworkError ->
            "The tag edit server could not be reached."

        Http.BadBody _ ->
            "The tag edit response was invalid."


reactionErrorToString : Http.Error -> String
reactionErrorToString error =
    case error of
        Http.BadStatus 409 ->
            "Rating changed elsewhere. Reload the post and try again."

        Http.BadStatus 404 ->
            "One selected post no longer exists."

        Http.BadStatus status ->
            "Rating edit failed (HTTP " ++ String.fromInt status ++ ")."

        Http.BadUrl url ->
            "Invalid API URL: " ++ url

        Http.Timeout ->
            "The rating edit timed out."

        Http.NetworkError ->
            "The rating edit server could not be reached."

        Http.BadBody _ ->
            "The rating response was invalid."


saveTags : Model -> ( Model, Cmd Msg )
saveTags model =
    let
        ids =
            Selection.targets model.sequence model.selection

        targets =
            List.map (tagTarget model) ids

        add =
            splitTagDraft model.tagAddDraft

        remove =
            splitTagDraft model.tagRemoveDraft

        requestId =
            model.tagRequestId + 1
    in
    if not (App.Access.canWrite model.access) then
        ( { model | tagStatus = Just "Unlock to edit tags." }, Cmd.none )

    else if List.isEmpty targets then
        ( { model | tagStatus = Just "Select a post before editing tags." }, Cmd.none )

    else if List.isEmpty add && List.isEmpty remove then
        ( { model | tagStatus = Just "Enter a tag to add or remove." }, Cmd.none )

    else
        ( { model | tagSaving = True, tagStatus = Nothing, revertStatus = Nothing, tagRequestId = requestId }
        , Api.Post.editTags model.apiBase (App.Access.credential model.access) targets add remove (TagsCompleted model.access.generation requestId (List.map .id targets))
        )


requestRevert : Model -> Int -> ( Model, Cmd Msg )
requestRevert model targetVersion =
    if not (App.Access.canWrite model.access) then
        ( { model | revertStatus = Just "Unlock to revert tags." }, Cmd.none )

    else
        case model.selection.active of
            Nothing ->
                ( { model | tagStatus = Just "Select a post before reverting tags." }, Cmd.none )

            Just postId ->
                case Dict.get postId model.details of
                    Just (DetailReady detail) ->
                        case List.filter (\r -> r.version == targetVersion) detail.history |> List.head of
                            Just revision ->
                                if not revision.revertible then
                                    ( { model | revertStatus = Just "Legacy revision cannot be reverted." }, Cmd.none )

                                else if model.revertPending /= Nothing then
                                    ( model, Cmd.none )

                                else
                                    ( { model | revertConfirm = Just targetVersion, revertStatus = Nothing }, Cmd.none )

                            Nothing ->
                                ( { model | revertStatus = Just "Revision not found." }, Cmd.none )

                    _ ->
                        ( { model | revertConfirm = Just targetVersion, revertStatus = Nothing }, Cmd.none )


confirmRevert : Model -> Int -> ( Model, Cmd Msg )
confirmRevert model targetVersion =
    if not (App.Access.canWrite model.access) then
        ( { model | revertStatus = Just "Unlock to revert tags." }, Cmd.none )

    else
        case model.selection.active of
            Nothing ->
                ( { model | tagStatus = Just "Select a post before reverting tags." }, Cmd.none )

            Just postId ->
                let
                    requestId =
                        model.tagRequestId + 1

                    target =
                        tagTarget model postId
                in
                ( { model | tagSaving = True, revertPending = Just targetVersion, revertConfirm = Nothing, revertStatus = Nothing, tagStatus = Nothing, tagRequestId = requestId }
                , Api.Post.revertTags model.apiBase (App.Access.credential model.access) [ target ] targetVersion (TagsCompleted model.access.generation requestId [ postId ])
                )


tagTarget : Model -> String -> TagEditTarget
tagTarget model postId =
    { id = postId
    , version =
        case Dict.get postId model.tagVersions of
            Just version ->
                version

            Nothing ->
                Dict.get postId model.details
                    |> Maybe.andThen
                        (\state ->
                            case state of
                                DetailReady detail ->
                                    Just detail.tagVersion

                                _ ->
                                    Nothing
                        )
                    |> Maybe.withDefault 0

    }


reactionTarget : Model -> String -> ReactionTarget
reactionTarget model postId =
    { id = postId
    , version =
        case Dict.get postId model.reactionVersions of
            Just version ->
                version

            Nothing ->
                Dict.get postId model.details
                    |> Maybe.andThen
                        (\state ->
                            case state of
                                DetailReady detail ->
                                    Just detail.reactionVersion

                                _ ->
                                    Nothing
                        )
                    |> Maybe.withDefault 0
    }


saveFavorite : Model -> ( Model, Cmd Msg )
saveFavorite model =
    let
        ids =
            Selection.targets model.sequence model.selection

        targets =
            List.map (reactionTarget model) ids

        favorite =
            activeDetail model |> Maybe.map (\detail -> not detail.favorite)

        requestId =
            model.reactionRequestId + 1
    in
    if not (App.Access.canWrite model.access) then
        ( { model | reactionStatus = Just "Unlock to change favorites." }, Cmd.none )

    else
        case favorite of
            Nothing ->
                ( { model | reactionStatus = Just "Select a post and load its details before changing favorite." }, Cmd.none )

            Just value ->
                if List.isEmpty targets then
                    ( { model | reactionStatus = Just "Select a post before changing favorite." }, Cmd.none )

                else
                    ( { model | reactionSaving = True, reactionStatus = Nothing, reactionRequestId = requestId }
                    , Api.Post.editReactions model.apiBase (App.Access.credential model.access) targets (Just value) Nothing (ReactionsCompleted model.access.generation requestId (List.map .id targets))
                    )


saveScore : Model -> ( Model, Cmd Msg )
saveScore model =
    let
        ids =
            Selection.targets model.sequence model.selection

        targets =
            List.map (reactionTarget model) ids

        requestId =
            model.reactionRequestId + 1
    in
    if not (App.Access.canWrite model.access) then
        ( { model | reactionStatus = Just "Unlock to change the score." }, Cmd.none )

    else
        case String.toInt (String.trim model.scoreDraft) of
            Nothing ->
                ( { model | reactionStatus = Just "Score must be a non-negative integer." }, Cmd.none )

            Just score ->
                if score < 0 then
                    ( { model | reactionStatus = Just "Score must be a non-negative integer." }, Cmd.none )

                else if List.isEmpty targets then
                    ( { model | reactionStatus = Just "Select a post before changing score." }, Cmd.none )

                else
                    ( { model | reactionSaving = True, reactionStatus = Nothing, reactionRequestId = requestId }
                    , Api.Post.editReactions model.apiBase (App.Access.credential model.access) targets Nothing (Just score) (ReactionsCompleted model.access.generation requestId (List.map .id targets))
                    )


splitTagDraft : String -> List String
splitTagDraft draft =
    draft
        |> String.split ","
        |> List.map String.trim
        |> List.filter (String.isEmpty >> not)



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
                        Just ToggleFavoriteAction

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
        , Sub.map UploadMsg (UploadQueue.subscriptions model.upload)
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
        , div [ class "progress", classList [ ( "is-active", (model.search == Searching || model.search == LoadingMore) ) ] ] []
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
            , Html.map UploadMsg (UploadQueue.view (App.Access.canWrite model.access) model.upload)
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
        , accessControls model
        , uploadButton model
        , Ui.Button.view [ class "button-quiet" ]
            { label = "Info"
            , key = Just "I"
            , onPress = Just (KeyCommand ToggleInspector)
            , pressed = Just (inspectorVisible model)
            , hint = Just "Toggle inspector"
            }
        ]


uploadButton : Model -> Html Msg
uploadButton model =
    let
        pending =
            UploadQueue.pendingCount model.upload

        label =
            if pending == 0 then
                "Upload"

            else
                "Upload (" ++ String.fromInt pending ++ ")"
    in
    Ui.Button.view [ class "button-quiet" ]
        { label = label
        , key = Nothing
        , onPress =
            Just
                (UploadMsg
                    (if UploadQueue.isOpen model.upload then
                        UploadQueue.ClosePanel

                     else
                        UploadQueue.OpenPanel
                    )
                )
        , pressed = Just (UploadQueue.isOpen model.upload)
        , hint = Just "Upload images"
        }


{-| Compact write-access controls. Open-local mode shows a quiet label;
gated mode shows a token field with Unlock, or the unlocked state with
Lock. The token draft lives only in transient access state and clears
on validation; wrong tokens keep the browser locked with a fixed
message that never echoes the draft.
-}
accessControls : Model -> Html Msg
accessControls model =
    case model.access.status of
        App.Access.Checking ->
            span [ class "access-state" ] [ text "Checking access…" ]

        App.Access.OpenLocal ->
            span [ class "access-state", title "Local mode: writes need no token." ] [ text "Local" ]

        App.Access.Locked ->
            accessUnlockForm model False

        App.Access.Validating ->
            accessUnlockForm model True

        App.Access.Unlocked ->
            div [ class "access-block" ]
                [ span [ class "access-state", title "Writes enabled as the system owner." ] [ text "Unlocked" ]
                , Ui.Button.view [ class "button-quiet" ]
                    { label = "Lock"
                    , key = Nothing
                    , onPress = Just LockRequested
                    , pressed = Nothing
                    , hint = Just "Lock writes and clear the token"
                    }
                ]

        App.Access.Unavailable ->
            div [ class "access-block" ]
                [ span [ class "access-state" ] [ text "Access unavailable" ]
                , Ui.Button.view [ class "button-quiet" ]
                    { label = "Retry"
                    , key = Nothing
                    , onPress = Just AccessRetry
                    , pressed = Nothing
                    , hint = Just "Retry capability discovery"
                    }
                ]


accessUnlockForm : Model -> Bool -> Html Msg
accessUnlockForm model busy =
    form [ class "access-block", onSubmit UnlockRequested ]
        [ input
            [ class "token-field"
            , type_ "password"
            , placeholder "Token"
            , value model.access.draft
            , onInput TokenDraftChanged
            , attribute "aria-label" "Access token"
            , attribute "autocomplete" "off"
            , disabled busy
            ]
            []
        , Ui.Button.view [ class "button-quiet" ]
            { label =
                if busy then
                    "Unlocking…"

                else
                    "Unlock"
            , key = Nothing
            , onPress =
                if busy then
                    Nothing

                else
                    Just UnlockRequested
            , pressed = Nothing
            , hint = Just "Validate the token and enable writes"
            }
        , case model.access.notice of
            Just notice ->
                span [ class "access-notice" ] [ text notice ]

            Nothing ->
                text ""
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

        LoadingMore ->
            "Loading more…"

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
            , saved = savedItems model
            , savedStatus = model.savedSearchStatus
            , savedSaving = model.savedSearchSaving
            , localFallback = model.savedSearchFallback
            , recent = model.recent
            , collections = model.collections
            , activeCollection = model.activeCollection
            , collectionDraft = model.collectionDraft
            , onCollectionDraft = CollectionDraftChanged
            , onCreateCollection = CreateCollection
            , onSelectCollection = SelectCollection
            , onOpenCollection = OpenCollection
            , onAllPosts = OpenAllPosts
            , onMoveCollectionPost = MoveCollectionPost
            , onRemoveCollectionPost = RemoveCollectionPost
            , removingPosts = model.collectionRemovals
            , onRun = RunQuery
            , onSave = SaveSearch
            , onRemove = RemoveSavedSearch
            , onClose = KeyCommand ToggleNavigator
            , writesEnabled = App.Access.canWrite model.access
            }

    else
        text ""


{-| Owner separation for the navigator: the local fallback shows only the
on-device queries, otherwise only the authenticated owner's server
entries. The two lists are never merged.
-}
savedItems : Model -> List Api.SavedSearch.VisibleItem
savedItems model =
    Api.SavedSearch.visible model.savedSearchFallback model.savedSearches model.prefs.savedSearches


inspectorView : Model -> Html Msg
inspectorView model =
    if inspectorVisible model then
        Feature.Inspector.view
            { apiBase = model.apiBase
            , presentation = inspectorPresentation model
            , active = activePost model
            , detail = activeDetail model
            , detailLoading = activeDetailLoading model
            , detailError = activeDetailError model
            , selectedCount = Selection.count model.selection
            , commonTags = commonTags model
            , missing = model.missing
            , onTag = TagClicked
            , onClear = KeyCommand ClearSelection
            , onClose = KeyCommand ToggleInspector
            , onApiPending = KeyCommand << Pending
            , onFavorite = KeyCommand ToggleFavoriteAction
            , onAddToCollection = AddToCollection
            , onMediaError = MediaFailed
            , writesEnabled = App.Access.canWrite model.access
            , tagAdd = model.tagAddDraft
            , tagRemove = model.tagRemoveDraft
            , tagStatus = model.tagStatus
            , tagSaving = model.tagSaving
            , onTagAdd = TagAddChanged
            , onTagRemove = TagRemoveChanged
            , onSaveTags = SaveTags
            , scoreDraft = model.scoreDraft
            , reactionSaving = model.reactionSaving
            , reactionStatus = model.reactionStatus
            , onScoreDraft = ScoreDraftChanged
            , onSaveScore = SaveScore
            , onRevert = RevertTags
            , onConfirmRevert = ConfirmRevert
            , onCancelRevert = CancelRevert
            , revertConfirm = model.revertConfirm
            , revertPending = model.revertPending
            , revertStatus = model.revertStatus
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
            , stale = (model.search == Searching || model.search == LoadingMore) && not (Sequence.isEmpty model.sequence)
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

                LoadingMore ->
                    [ p [] [ text "Loading more…" ] ]

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
