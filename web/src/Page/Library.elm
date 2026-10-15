module Page.Library exposing (Model, Msg, init, onUrlChange, onUrlRequest, subscriptions, update, view)

import Api.ModerationPreview as ModerationPreview
import Api.Post
import Api.Access
import Api.Error as ApiError
import Api.Collection
import Api.SavedSearch
import App.Access
import App.Keyboard as Keyboard exposing (KeyEvent, Modifiers, Target(..))
import App.PanelEscape as PanelEscape
import App.Prefs as Prefs exposing (Prefs)
import App.Route as Route exposing (Route, View(..))
import Browser
import Browser.Dom as Dom
import Browser.Events
import Browser.Navigation as Nav
import Domain.Moderation
import Domain.Session as AccountSession
import Domain.Post exposing (PostDetail, PostSummary, SearchResponse, summaryOfDetail)
import Domain.CollectionLogic as CollectionLogic
import Domain.Query as Query
import Domain.Selection as Selection exposing (Selection)
import Domain.Sequence as Sequence exposing (Sequence)
import Feature.Compare
import Feature.Filmstrip as Filmstrip
import Feature.Import as BrowserImport
import Feature.Inspector
import Feature.BulkMetadata as BulkMetadata
import Feature.Duplicates as Duplicates
import Feature.Moderation as Moderation
import Feature.Recovery as Recovery
import Feature.Session as Session
import Feature.Similarity as Similarity
import Feature.Metadata as Metadata
import Feature.MediaGrid as MediaGrid
import Feature.MediaGrid.Layout as Layout exposing (Geometry, Viewport)
import Feature.Navigator
import Feature.Collections as Collections
import Feature.QueryEditor as QueryEditor
import Feature.Ratings as Ratings
import Feature.Reactions as Reactions
import Feature.QuickLook as QuickLook exposing (Zoom(..))
import Feature.Search as Search
import Feature.Shortcuts as Shortcuts
import Feature.Export as Export
import Feature.TagSuggestions as TagSuggestions
import Feature.Tags as Tags
import Feature.Trash as Trash
import Feature.TagManagement as TagManagement
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
import Ui.Icon
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


type alias ReturnPoint =
    { scrollTop : Float
    , activeAtEntry : Maybe String
    }


type DetailState
    = DetailLoading
    | DetailReady PostDetail
    | DetailFailed String


type alias Model =
    { key : Nav.Key
    , apiBase : String
    , draftQuery : String
    , search : Search.Model
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
    , tags : Tags.Model
    , reactions : Reactions.Model
    , ratings : Ratings.Model
    , collections : Collections.Model
    , savedSearches : List Api.SavedSearch.SavedSearch
    , savedSearchFallback : Bool
    , savedSearchSaving : Bool
    , savedSearchStatus : Maybe String
    , upload : UploadQueue.Model
    , browserImport : BrowserImport.Model
    , metadata : Metadata.Model
    , bulkMetadata : BulkMetadata.Model
    , duplicates : Duplicates.Model
    , moderation : Moderation.Model
    , session : Session.Model
    , similarity : Similarity.Model
    , recovery : Recovery.Model
    , similarityTrash : Maybe ( String, List String, Int )
    , similarityCompare : Maybe ( String, String )
    , uploadedPosts : Dict String PostSummary
    , uploadOrigin : Maybe UploadOrigin
    , duplicatesOrigin : Maybe UploadOrigin
    , duplicateTrash : Maybe ( String, List String )
    , access : App.Access.State
    , trash : Trash.Model
    , tagManagement : TagManagement.Model
    , detailEpoch : Int
    , exportMetadata : Bool
    , tagSuggestions : TagSuggestions.Model
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

        ( sessionModel, sessionCmd ) =
            Session.init (Session.context flags.apiBase 0)

        model =
            { key = key
            , apiBase = flags.apiBase
            , draftQuery = route.query
            , search = Search.init
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
            , tags = Tags.init
            , reactions = Reactions.init
            , ratings = Ratings.init
            , collections = Collections.init
            , savedSearches = []
            , savedSearchFallback = True
            , savedSearchSaving = False
            , savedSearchStatus = Nothing
            , upload = UploadQueue.init
            , browserImport = BrowserImport.init
            , metadata = Metadata.init
            , bulkMetadata = BulkMetadata.init
            , duplicates = Duplicates.init
            , moderation = Moderation.init
            , session = sessionModel
            , similarity = Similarity.init
            , recovery = Recovery.init
            , similarityTrash = Nothing
            , similarityCompare = Nothing
            , uploadedPosts = Dict.empty
            , uploadOrigin = Nothing
            , duplicatesOrigin = Nothing
            , duplicateTrash = Nothing
            , access = App.Access.init
            , trash = Trash.init
            , tagManagement = TagManagement.init
            , detailEpoch = 0
            , exportMetadata = True
            , tagSuggestions = TagSuggestions.init
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
        , Cmd.map SessionMsg sessionCmd
        , Api.Collection.summaries flags.apiBase (CollectionsMsg << Collections.SummariesCompleted 0)
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
    | FocusTags
    | AddSelectionToCollection


type Msg
    = UrlChanged Url
    | UrlRequested Browser.UrlRequest
    | DraftChanged String
    | SearchSubmitted
    | SearchMsg Search.Msg
    | DetailCompleted Int Int String (Result Http.Error PostDetail)
    | TagsMsg Tags.Msg
    | ReactionsMsg Reactions.Msg
    | RatingsMsg Ratings.Msg
    | CollectionsMsg Collections.Msg
    | CapabilitiesReceived Int (Result Http.Error Api.Access.Capabilities)
    | TokenDraftChanged String
    | UnlockRequested
    | LockRequested
    | AccessRetry
    | SavedSearchesCompleted Int (Result Http.Error (List Api.SavedSearch.SavedSearch))
    | SavedSearchCreated Int (Result Http.Error Api.SavedSearch.SavedSearch)
    | SavedSearchDeleted Int String (Result Http.Error ())
    | OpenCollection String
    | OpenAllPosts
    | AddToCollection
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
    | ThemeToggled
    | OrderChanged String
    | SaveSearch
    | RemoveSavedSearch String
    | SyncUrl Int
    | UploadMsg UploadQueue.Msg
    | ImportMsg BrowserImport.Msg
    | MetadataMsg Metadata.Msg
    | BulkMetadataMsg BulkMetadata.Msg
    | TrashMsg Trash.Msg
    | ModerationMsg Moderation.Msg
    | SessionMsg Session.Msg
    | SimilarityMsg Similarity.Msg
    | RecoveryMsg Recovery.Msg
    | SimilarityCompareMeasured String String (Result Dom.Error Dom.Viewport)
    | DuplicatesMsg Duplicates.Msg
    | DuplicateCompareMeasured String String String (Result Dom.Error Dom.Viewport)
    | TagManagementMsg TagManagement.Msg
    | ExportMetadataChanged Bool
    | TagSuggestionMsg TagSuggestions.Msg
    | TagSuggestionChosen String
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

        syncedRatings =
            if updated.selection /= model.selection then
                { updated | ratings = Ratings.selectionChanged (ratingTargetSummaries updated) updated.ratings }

            else
                updated

        ( next, detailCmd ) =
            if syncedRatings.selection.active /= model.selection.active then
                prepareActiveDetail syncedRatings

            else
                ( syncedRatings, Cmd.none )

        bulkIds =
            bulkTargetIds next

        ( bulkSynced, bulkCmd ) =
            if bulkIds /= BulkMetadata.ownerIds next.bulkMetadata || BulkMetadata.wantsRead next.bulkMetadata then
                let
                    ( bulk, bulkInnerCmd, bulkOuts ) =
                        BulkMetadata.update (bulkMetadataContext next) (BulkMetadata.SelectionSync bulkIds) next.bulkMetadata
                in
                handleBulkMetadataOuts bulkOuts ( { next | bulkMetadata = bulk }, Cmd.map BulkMetadataMsg bulkInnerCmd )

            else
                ( next, Cmd.none )

        ( singleRefreshed, singleCmd ) =
            if List.length (BulkMetadata.ownerIds model.bulkMetadata) >= 2 && List.length bulkIds < 2 then
                case bulkSynced.selection.active of
                    Just postId ->
                        ( { bulkSynced
                            | metadata = Metadata.activeChanged bulkSynced.metadata
                            , details = Dict.insert postId DetailLoading bulkSynced.details
                          }
                        , Api.Post.detail bulkSynced.apiBase postId (DetailCompleted bulkSynced.access.generation bulkSynced.detailEpoch postId)
                        )

                    Nothing ->
                        ( bulkSynced, Cmd.none )

            else
                ( bulkSynced, Cmd.none )

        urlFollows =
            case msg of
                UrlChanged _ ->
                    False

                SyncUrl _ ->
                    False

                _ ->
                    currentRoute singleRefreshed /= currentRoute model

        ( synced, syncCmd ) =
            if urlFollows then
                ( { singleRefreshed | urlSeq = singleRefreshed.urlSeq + 1 }
                , Process.sleep 250 |> Task.perform (\_ -> SyncUrl (singleRefreshed.urlSeq + 1))
                )

            else
                ( singleRefreshed, Cmd.none )

        filmstripCmd =
            case ( synced.mode, synced.selection.active ) of
                ( Grid, _ ) ->
                    Cmd.none

                ( _, Just postId ) ->
                    if synced.selection.active /= model.selection.active || model.mode == Grid then
                        centerFilmstrip postId

                    else
                        Cmd.none

                ( _, Nothing ) ->
                    Cmd.none

    in
    ( synced, Cmd.batch [ cmd, syncCmd, filmstripCmd, detailCmd, bulkCmd, singleCmd ] )


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
                    invalidateSimilarityTrash { model | ownQuery = Nothing }
            in
            if route.query /= Search.query model.search || own then
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

        OrderChanged requested ->
            runQuery (Query.setOrder requested (Search.query model.search)) model

        RunQuery query ->
            runQuery query { model | navigatorDrawer = False }

        OpenAllPosts ->
            runQuery "" model

        OpenCollection collectionId ->
            delegateCollections (Collections.OpenRequested collectionId)
                { model | navigatorDrawer = False, draftQuery = "" }

        CollectionsMsg subMsg ->
            delegateCollections subMsg model

        AddToCollection ->
            delegateCollections Collections.AddRequested model

        TagClicked tag exclude ->
            runQuery
                (if exclude then
                    Query.excludeTerm tag (Search.query model.search)

                 else
                    Query.addTerm tag (Search.query model.search)
                )
                model

        TermRemoved index ->
            runQuery (Query.removeTerm index (Search.query model.search)) model

        SearchMsg subMsg ->
            delegateSearch subMsg model

        ThemeToggled ->
            savePrefs (\prefs -> { prefs | darkMode = not prefs.darkMode }) model

        DetailCompleted generation detailEpoch postId result ->
            if not (App.Access.isCurrent generation model.access) || detailEpoch /= model.detailEpoch then
                ( model, Cmd.none )

            else if Session.privateMode model.session && resultIsUnauthorized result then
                accessRevokedNow model

            else
                let
                    effectiveResult =
                        case result of
                            Ok detail ->
                                Ok (BulkMetadata.protectDetail model.bulkMetadata (guardRatingDetail model postId detail))

                            Err error ->
                                Err error
                in
                ( { model
                    | sequence =
                        case effectiveResult of
                            Ok detail ->
                                Sequence.refreshSummary (summaryOfDetail detail) model.sequence

                            Err _ ->
                                model.sequence
                    , details =
                        Dict.insert postId
                            (case effectiveResult of
                                Ok detail ->
                                    DetailReady detail

                                Err error ->
                                    DetailFailed (httpErrorToString error)
                            )
                            model.details
                    , tags =
                        case effectiveResult of
                            Ok detail ->
                                Tags.noteVersion postId detail.tagVersion model.tags

                            Err _ ->
                                model.tags
                    , reactions =
                        case effectiveResult of
                            Ok detail ->
                                let
                                    reactions =
                                        Reactions.noteVersion postId detail.reactionVersion model.reactions
                                in
                                if model.selection.active == Just postId then
                                    Reactions.syncDraft (Just detail) reactions

                                else
                                    reactions

                            Err _ ->
                                model.reactions
                    , ratings =
                        case effectiveResult of
                            Ok detail ->
                                Ratings.noteDetail detail model.ratings

                            Err _ ->
                                model.ratings
                    , metadata =
                        case effectiveResult of
                            Ok detail ->
                                if model.selection.active == Just postId then
                                    Metadata.syncDetail detail model.metadata

                                else
                                    model.metadata

                            Err _ ->
                                model.metadata
                    , bulkMetadata =
                        case effectiveResult of
                            Ok detail ->
                                BulkMetadata.noteDetail detail model.bulkMetadata

                            Err _ ->
                                model.bulkMetadata
                  }
                , Cmd.none
                )

        TagsMsg subMsg ->
            let
                ( tagged, tagCmd ) =
                    delegateTags subMsg model

                ( sugg, suggCmd ) =
                    case subMsg of
                        Tags.AddChanged draft ->
                            TagSuggestions.update (TagSuggestions.context tagged.apiBase) (TagSuggestions.QueryChanged draft) tagged.tagSuggestions

                        _ ->
                            ( tagged.tagSuggestions, Cmd.none )
            in
            ( { tagged | tagSuggestions = sugg }, Cmd.batch [ tagCmd, Cmd.map TagSuggestionMsg suggCmd ] )

        TrashMsg subMsg ->
            case subMsg of
                Trash.TrashSelectedRequested _ ->
                    delegateTrash subMsg { model | duplicateTrash = Nothing, similarityTrash = Nothing }

                _ ->
                    delegateTrash subMsg model

        ModerationMsg subMsg ->
            delegateModeration subMsg model

        SessionMsg subMsg ->
            delegateSession subMsg model

        SimilarityMsg subMsg ->
            delegateSimilarity subMsg model

        RecoveryMsg subMsg ->
            delegateRecovery subMsg model

        SimilarityCompareMeasured anchorId candidateId result ->
            handleSimilarityCompareMeasured anchorId candidateId result model

        DuplicatesMsg subMsg ->
            delegateDuplicates subMsg model

        DuplicateCompareMeasured hash select candidate result ->
            if Duplicates.currentHash model.duplicates /= Just hash
                || not (Duplicates.isOpen model.duplicates)
                || Duplicates.membersLoading model.duplicates
                || not (List.all (\id -> List.member id (List.map .id (Duplicates.members model.duplicates))) [ select, candidate ]) then
                ( model, Cmd.none )

            else
                let
                    ( opened, command ) =
                        openDuplicatesCompare select candidate (Duplicates.members model.duplicates) model

                    top =
                        result |> Result.map (\viewport -> viewport.viewport.y) |> Result.withDefault model.viewport.scrollTop
                in
                ( { opened | returnPoint = Maybe.map (\point -> { point | scrollTop = top }) opened.returnPoint }, command )

        TagManagementMsg subMsg ->
            delegateTagManagement subMsg model

        ExportMetadataChanged value ->
            ( { model | exportMetadata = value }, Cmd.none )

        TagSuggestionMsg subMsg ->
            let
                ( sugg, cmd ) =
                    TagSuggestions.update (TagSuggestions.context model.apiBase) subMsg model.tagSuggestions
            in
            ( { model | tagSuggestions = sugg }, Cmd.map TagSuggestionMsg cmd )

        TagSuggestionChosen tag ->
            let
                newDraft =
                    TagSuggestions.applySuggestion (Tags.addDraft model.tags) tag

                ( tagged, tagCmd ) =
                    delegateTags (Tags.AddChanged newDraft) model
            in
            ( { tagged | tagSuggestions = TagSuggestions.clear tagged.tagSuggestions }, tagCmd )

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

        ReactionsMsg subMsg ->
            delegateReactions subMsg model

        RatingsMsg subMsg ->
            delegateRatings subMsg model

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

        Retry ->
            case Collections.browse model.collections of
                Just _ ->
                    delegateCollections Collections.RetryRequested model

                Nothing ->
                    retrySearch model

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
                    String.trim (Search.query model.search)
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

        ImportMsg subMsg ->
            delegateImport subMsg model

        MetadataMsg subMsg ->
            delegateMetadata subMsg model

        BulkMetadataMsg subMsg ->
            delegateBulkMetadata subMsg model

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
stay in the queue; sequence members are refreshed in place, but new
posts are never injected into filtered searches or collection results.
The embedded detail comes from the upload transaction, so its tags and
tag version may predate a global rename that landed meanwhile: it is
never trusted for the detail or tag-version caches. A fresh detail
GET under the current detail epoch repopulates both, while the
uploaded-post summary keeps the route and the rating/reaction metadata
from the confirmation is preserved.
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
            let
                guarded =
                    guardRatingDetail model detail.id detail

                freshDetail =
                    Api.Post.detail model.apiBase guarded.id (DetailCompleted model.access.generation model.detailEpoch guarded.id)
            in
            ( { model
                | details = Dict.insert guarded.id DetailLoading model.details
                , uploadedPosts = Dict.insert guarded.id (summaryOfDetail guarded) model.uploadedPosts
                , reactions = Reactions.noteVersion guarded.id guarded.reactionVersion model.reactions
                , ratings = Ratings.noteDetail guarded model.ratings
              }
            , Cmd.batch [ cmd, freshDetail ]
            )

        UploadQueue.ThumbCompleted postId ->
            ( model
            , Cmd.batch [ cmd, Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation model.detailEpoch postId) ]
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

importContext : Model -> BrowserImport.Context
importContext model =
    BrowserImport.context model.apiBase (App.Access.credential model.access) (App.Access.canWrite model.access) model.access.generation (importOwner model)


importOwner : Model -> String
importOwner model =
    case model.access.status of
        App.Access.OpenLocal ->
            "local"

        App.Access.Unlocked ->
            model.session.session |> Maybe.andThen AccountSession.accountId |> Maybe.withDefault ""

        _ ->
            ""


delegateImport : BrowserImport.Msg -> Model -> ( Model, Cmd Msg )
delegateImport subMsg model =
    let
        ( importModel, importCmd, outs ) =
            BrowserImport.update (importContext model) subMsg model.browserImport
    in
    handleImportOuts outs ( { model | browserImport = importModel }, Cmd.map ImportMsg importCmd )


handleImportOuts : List BrowserImport.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleImportOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleImportOuts rest (applyImportOut out ( model, cmd ))


{-| Import confirmations keep the workspace stable: the captured
collection is reconciled only while it is still browsed. As with
uploads, the embedded detail may carry tags from before a global
rename, so a fresh detail GET under the current detail epoch fills
the detail and tag-version caches; the uploaded-post summary keeps
the route and rating/reaction metadata is preserved. Summaries are
public, so attachment changes refresh them without a credential.
-}
applyImportOut : BrowserImport.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyImportOut out ( model, cmd ) =
    case out of
        BrowserImport.ImportConfirmed result ->
            let
                detail =
                    guardRatingDetail model result.post.id result.post

                freshDetail =
                    Api.Post.detail model.apiBase detail.id (DetailCompleted model.access.generation model.detailEpoch detail.id)

                base =
                    { model
                        | details = Dict.insert detail.id DetailLoading model.details
                        , uploadedPosts = Dict.insert detail.id (summaryOfDetail detail) model.uploadedPosts
                        , reactions = Reactions.noteVersion detail.id detail.reactionVersion model.reactions
                        , ratings = Ratings.noteDetail detail model.ratings
                    }
            in
            case result.collectionId of
                Just collectionId ->
                    let
                        ( collections, summariesCmd ) =
                            Collections.requestSummaries (collectionsContext base) base.collections

                        withSummaries =
                            { base | collections = collections }
                    in
                    if Collections.browse withSummaries.collections == Just collectionId then
                        delegateCollections Collections.RetryRequested withSummaries
                            |> withCmd (Cmd.batch [ cmd, Cmd.map CollectionsMsg summariesCmd, freshDetail ])

                    else
                        ( withSummaries, Cmd.batch [ cmd, Cmd.map CollectionsMsg summariesCmd, freshDetail ] )

                Nothing ->
                    ( base, Cmd.batch [ cmd, freshDetail ] )

        BrowserImport.OpenPost postId ->
            case Dict.get postId model.uploadedPosts |> Maybe.map Just |> Maybe.withDefault (Sequence.find postId model.sequence) of
                Just summary ->
                    openUploadPost postId summary ( model, cmd )

                Nothing ->
                    ( { model | hint = Just "That import is no longer available in this tab." }, cmd )

        BrowserImport.CredentialRejected requestGeneration status ->
            if status == 401 && App.Access.isCurrent requestGeneration model.access && App.Access.canWrite model.access then
                revokeAccess model |> withCmd cmd

            else
                ( model, cmd )


metadataContext : Model -> Metadata.Context
metadataContext model =
    Metadata.context
        model.apiBase
        (App.Access.credential model.access)
        (App.Access.canWrite model.access)
        model.access.generation
        model.selection.active
        (activeDetail model)


delegateMetadata : Metadata.Msg -> Model -> ( Model, Cmd Msg )
delegateMetadata subMsg model =
    let
        ( metadata, metadataCmd, outs ) =
            Metadata.update (metadataContext model) subMsg model.metadata
    in
    handleMetadataOuts outs ( { model | metadata = metadata }, Cmd.map MetadataMsg metadataCmd )


handleMetadataOuts : List Metadata.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleMetadataOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleMetadataOuts rest (applyMetadataOut out ( model, cmd ))


applyMetadataOut : Metadata.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyMetadataOut out ( model, cmd ) =
    case out of
        Metadata.Saved result ->
            let
                details =
                    Dict.update result.id
                        (\state ->
                            case state of
                                Just (DetailReady detail) ->
                                    Just
                                        (DetailReady
                                            { detail
                                                | source = result.source
                                                , artist = result.artist
                                                , metadataVersion = result.version
                                            }
                                        )

                                other ->
                                    other
                        )
                        model.details

                base =
                    { model
                        | details = details
                        , bulkMetadata = BulkMetadata.noteSnapshot { id = result.id, version = result.version, source = result.source, artist = result.artist } model.bulkMetadata
                    }

                detailCmd =
                    Api.Post.detail model.apiBase result.id (DetailCompleted model.access.generation model.detailEpoch result.id)
            in
            if not result.changed then
                ( base, Cmd.batch [ cmd, detailCmd ] )

            else
                case Collections.browse base.collections of
                    Just _ ->
                        delegateCollections Collections.RetryRequested base
                            |> withCmd (Cmd.batch [ cmd, detailCmd ])

                    Nothing ->
                        if Search.state base.search == Search.Idle then
                            ( base, Cmd.batch [ cmd, detailCmd ] )

                        else
                            retrySearch base |> withCmd (Cmd.batch [ cmd, detailCmd ])

        Metadata.StaleResponse postId ->
            refreshPublicPosts [ postId ] model |> withCmd cmd

        Metadata.AccessRevoked ->
            revokeAccess model |> withCmd cmd

        Metadata.ReloadPost postId ->
            ( { model | details = Dict.insert postId DetailLoading model.details }
            , Cmd.batch [ cmd, Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation model.detailEpoch postId) ]
            )


bulkTargetIds : Model -> List String
bulkTargetIds model =
    Selection.targets model.sequence model.selection
        |> List.filterMap (\postId -> Sequence.find postId model.sequence)
        |> List.map .id


bulkMetadataContext : Model -> BulkMetadata.Context
bulkMetadataContext model =
    BulkMetadata.context
        model.apiBase
        (App.Access.credential model.access)
        (App.Access.canWrite model.access)
        model.access.generation


delegateBulkMetadata : BulkMetadata.Msg -> Model -> ( Model, Cmd Msg )
delegateBulkMetadata subMsg model =
    let
        ( bulk, bulkCmd, outs ) =
            BulkMetadata.update (bulkMetadataContext model) subMsg model.bulkMetadata
    in
    handleBulkMetadataOuts outs ( { model | bulkMetadata = bulk }, Cmd.map BulkMetadataMsg bulkCmd )


handleBulkMetadataOuts : List BulkMetadata.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleBulkMetadataOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleBulkMetadataOuts rest (applyBulkMetadataOut out ( model, cmd ))


applyBulkMetadataOut : BulkMetadata.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyBulkMetadataOut out ( model, cmd ) =
    case out of
        BulkMetadata.Saved results capturedIds ->
            let
                details =
                    List.foldl
                        (\post dict ->
                            Dict.update post.id
                                (\state ->
                                    case state of
                                        Just (DetailReady detail) ->
                                            if post.version >= detail.metadataVersion then
                                                Just (DetailReady { detail | source = post.source, artist = post.artist, metadataVersion = post.version })

                                            else
                                                state

                                        other ->
                                            other
                                )
                                dict
                        )
                        model.details
                        results

                base =
                    { model | details = details }

                ( refreshed, refreshCmd ) =
                    refreshQueryMembership base

                detailCmd =
                    case model.selection.active of
                        Just activeId ->
                            if List.member activeId capturedIds then
                                Api.Post.detail model.apiBase activeId (DetailCompleted model.access.generation model.detailEpoch activeId)

                            else
                                Cmd.none

                        Nothing ->
                            Cmd.none
            in
            ( refreshed, Cmd.batch [ cmd, refreshCmd, detailCmd ] )

        BulkMetadata.Uncertain postIds ->
            let
                ( refreshed, refreshCmd ) =
                    refreshPublicPosts postIds model
            in
            refreshQueryMembership refreshed |> withCmd (Cmd.batch [ cmd, refreshCmd ])

        BulkMetadata.StaleResponse postIds ->
            let
                ( refreshed, refreshCmd ) =
                    refreshPublicPosts postIds model
            in
            refreshQueryMembership refreshed |> withCmd (Cmd.batch [ cmd, refreshCmd ])

        BulkMetadata.AccessRevoked ->
            revokeAccess model |> withCmd cmd


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
            | sequence = Sequence.fromList [ effectiveRatingSummary model summary ]
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
            case
                PanelEscape.next
                    { browserImport = BrowserImport.isOpen model.browserImport
                    , upload = UploadQueue.isOpen model.upload
                    , duplicates = Duplicates.isOpen model.duplicates
                    , similarity = Similarity.isOpen model.similarity
                    , moderation = Moderation.isOpen model.moderation
                    , recovery = Recovery.isOpen model.recovery
                    , drawers = drawersOpen model
                    , grid = model.mode == Grid
                    }
            of
                PanelEscape.CloseImport ->
                    ( { model | browserImport = BrowserImport.setOpen False model.browserImport }, Cmd.none )

                PanelEscape.CloseUpload ->
                    ( { model | upload = UploadQueue.setOpen False model.upload }, Cmd.none )

                PanelEscape.CloseDuplicates ->
                    delegateDuplicates Duplicates.CloseRequested model

                PanelEscape.CloseSimilarity ->
                    delegateSimilarity Similarity.CloseRequested model

                PanelEscape.CloseModeration ->
                    delegateModeration Moderation.CloseRequested model

                PanelEscape.CloseRecovery ->
                    delegateRecovery Recovery.CloseRequested model

                PanelEscape.CloseDrawers ->
                    ( { model | navigatorDrawer = False, inspectorDrawer = False }, Cmd.none )

                PanelEscape.ExitMode ->
                    exitToGrid model

                PanelEscape.Idle ->
                    ( model, Cmd.none )

        EscapeField ->
            ( { model | draftQuery = Search.query model.search }, Task.attempt (\_ -> NoOp) (Dom.blur QueryEditor.fieldId) )

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
            delegateReactions Reactions.FavoriteRequested model

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

        FocusTags ->
            if model.selection.active == Nothing || not (App.Access.canWrite model.access) then
                ( model, Cmd.none )

            else if inspectorPresentation model == Docked then
                savePrefs (\prefs -> { prefs | inspectorDocked = True }) model
                    |> withCmd (Cmd.batch [ focusLater "tags-to-add", measureGrid 40 ])

            else
                ( { model | inspectorDrawer = True, navigatorDrawer = False }, focusLater "tags-to-add" )

        AddSelectionToCollection ->
            delegateCollections Collections.AddRequested model



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


searchContext : Model -> Search.Context
searchContext model =
    Search.context model.apiBase


{-| Route-driven search submission: the page keeps the draft, the pending
route, and collection workspace clearing; the feature owns the request
lifecycle, loading state, cursor, and stale-response rejection.
-}
startSearch : Model -> String -> ( Model, Cmd Msg )
startSearch model query =
    let
        ( search, searchCmd ) =
            Search.start (searchContext model) query model.search
    in
    ( { model | draftQuery = query, search = search, collections = Collections.abandonBrowse model.collections }
    , Cmd.map SearchMsg searchCmd
    )


{-| Manual retry of the current ordinary search. Collection browsing is
already empty on this path; a stale refresh marker clears so it cannot
linger past the fresh submission.
-}
retrySearch : Model -> ( Model, Cmd Msg )
retrySearch model =
    let
        ( search, searchCmd ) =
            Search.restart (searchContext model) model.search
    in
    ( { model | draftQuery = Search.query search, search = search, collections = Collections.abandonBrowse model.collections }
    , Cmd.map SearchMsg searchCmd
    )


delegateSearch : Search.Msg -> Model -> ( Model, Cmd Msg )
delegateSearch subMsg model =
    case subMsg of
        Search.Completed requestId _ result ->
            if Session.privateMode model.session && Session.canRead model.session && requestId == Search.requestId model.search && resultIsUnauthorized result then
                accessRevokedNow model

            else
                settleSearch subMsg model


resultIsUnauthorized : Result Http.Error value -> Bool
resultIsUnauthorized result =
    case result of
        Err (Http.BadStatus 401) ->
            True

        _ ->
            False


settleSearch : Search.Msg -> Model -> ( Model, Cmd Msg )
settleSearch subMsg model =
    let
        ( search, searchCmd, outs ) =
            Search.update (searchContext model) subMsg model.search
    in
    handleSearchOuts outs ( { model | search = search }, Cmd.map SearchMsg searchCmd )


handleSearchOuts : List Search.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleSearchOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleSearchOuts rest (applySearchOut out ( model, cmd ))


applySearchOut : Search.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applySearchOut out ( model, cmd ) =
    -- A browsed collection always wins: late search responses that slip
    -- past the search generation are dropped instead of replacing
    -- collection results.
    if Collections.browse model.collections /= Nothing then
        ( model, cmd )

    else
        case out of
            Search.InitialArrived response ->
                resultsArrived response model |> withCmd cmd

            Search.MoreArrived response ->
                moreResultsArrived response model |> withCmd cmd

            Search.PrefixArrived response ->
                prefixResultsArrived response model |> withCmd cmd

            Search.SearchFailed _ ->
                ( { model | pendingRoute = Nothing }, cmd )


{-| Per-call tag context: the current credential and write state, the
selection-derived edit targets, and detail facts for version checks and
revert history. The feature never stores any of it.
-}
tagsContext : Model -> Tags.Context
tagsContext model =
    Tags.context
        model.apiBase
        (App.Access.credential model.access)
        (App.Access.canWrite model.access)
        model.access.generation
        (Selection.targets model.sequence model.selection)
        model.selection.active
        (activeDetail model)
        (\postId ->
            Dict.get postId model.details
                |> Maybe.andThen
                    (\state ->
                        case state of
                            DetailReady detail ->
                                Just detail.tagVersion

                            _ ->
                                Nothing
                    )
        )


delegateTags : Tags.Msg -> Model -> ( Model, Cmd Msg )
delegateTags subMsg model =
    let
        ( tags, tagsCmd, outs ) =
            Tags.update (tagsContext model) subMsg model.tags
    in
    handleTagsOuts outs ( { model | tags = tags }, Cmd.map TagsMsg tagsCmd )


handleTagsOuts : List Tags.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleTagsOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleTagsOuts rest (applyTagsOut out ( model, cmd ))


{-| Tag save effects keep the workspace stable: edited summaries merge in
place, the active detail reloads, and stale-generation completions
reconcile their captured posts. The mutation itself is never replayed.
-}
applyTagsOut : Tags.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyTagsOut out ( model, cmd ) =
    case out of
        Tags.Saved response activeId ->
            let
                sequence =
                    List.foldl
                        (\post sequence_ -> Sequence.updateTags post.id post.tags sequence_)
                        model.sequence
                        response.posts

                ( refreshed, managementCmd ) =
                    delegateTagManagement TagManagement.TagsChanged { model | sequence = sequence }
            in
            case activeId of
                Just postId ->
                    ( { refreshed | details = Dict.insert postId DetailLoading refreshed.details }
                    , Cmd.batch [ cmd, managementCmd, Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation model.detailEpoch postId) ]
                    )

                Nothing ->
                    ( refreshed, Cmd.batch [ cmd, managementCmd ] )

        Tags.StaleResponse postIds ->
            refreshPublicPosts postIds model |> withCmd cmd

        Tags.AccessRevoked ->
            revokeAccess model |> withCmd cmd


{-| Per-call reactions context: the current credential and write state,
the selection-derived edit targets, the toggled favorite from the active
detail, and a detail-version fallback for version checks. The feature
never stores any of it.
-}
reactionsContext : Model -> Reactions.Context
reactionsContext model =
    Reactions.context
        model.apiBase
        (App.Access.credential model.access)
        (App.Access.canWrite model.access)
        model.access.generation
        (Selection.targets model.sequence model.selection)
        model.selection.active
        (activeDetail model |> Maybe.map (\detail -> not detail.favorite))
        (\postId ->
            Dict.get postId model.details
                |> Maybe.andThen
                    (\state ->
                        case state of
                            DetailReady detail ->
                                Just detail.reactionVersion

                            _ ->
                                Nothing
                    )
        )


delegateReactions : Reactions.Msg -> Model -> ( Model, Cmd Msg )
delegateReactions subMsg model =
    let
        ( reactions, reactionsCmd, outs ) =
            Reactions.update (reactionsContext model) subMsg model.reactions
    in
    handleReactionsOuts outs ( { model | reactions = reactions }, Cmd.map ReactionsMsg reactionsCmd )


handleReactionsOuts : List Reactions.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleReactionsOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleReactionsOuts rest (applyReactionsOut out ( model, cmd ))


{-| Reaction save effects keep the workspace stable: versions already
merged in the feature, the active detail reloads, and stale-generation
completions reconcile their captured posts. The mutation itself is never
replayed.
-}
applyReactionsOut : Reactions.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyReactionsOut out ( model, cmd ) =
    case out of
        Reactions.Saved activeId ->
            case activeId of
                Just postId ->
                    ( { model | details = Dict.insert postId DetailLoading model.details }
                    , Cmd.batch [ cmd, Api.Post.detail model.apiBase postId (DetailCompleted model.access.generation model.detailEpoch postId) ]
                    )

                Nothing ->
                    ( model, cmd )

        Reactions.StaleResponse postIds ->
            refreshPublicPosts postIds model |> withCmd cmd

        Reactions.AccessRevoked ->
            revokeAccess model |> withCmd cmd


ratingTargetSummaries : Model -> List PostSummary
ratingTargetSummaries model =
    Selection.targets model.sequence model.selection
        |> List.filterMap (\postId -> Sequence.find postId model.sequence)
        |> List.map (effectiveRatingSummary model)


{-| The canonical rating/version pair for an incoming summary: a known
pair at a higher version wins atomically; every other field is untouched.
-}
effectiveRatingSummary : Model -> PostSummary -> PostSummary
effectiveRatingSummary model summary =
    case ( Ratings.knownVersion summary.id model.ratings, Ratings.knownRating summary.id model.ratings ) of
        ( Just knownVersion_, Just knownRating_ ) ->
            case summary.ratingVersion of
                Just incoming ->
                    if incoming >= knownVersion_ then
                        summary

                    else
                        { summary | rating = knownRating_, ratingVersion = Just knownVersion_ }

                Nothing ->
                    { summary | rating = knownRating_, ratingVersion = Just knownVersion_ }

        _ ->
            summary


{-| Remember every accepted page's version/value pair before a delayed
Inspector read can arrive, even when the selection itself did not change.
-}
noteRatingSummaries : List PostSummary -> Model -> Model
noteRatingSummaries summaries model =
    { model | ratings = List.foldl Ratings.noteSummary model.ratings summaries }


effectiveRatingSummaries : Model -> List PostSummary -> List PostSummary
effectiveRatingSummaries model summaries =
    List.map (effectiveRatingSummary model) summaries


ratingTargets : Model -> List Ratings.TargetInfo
ratingTargets model =
    ratingTargetSummaries model
        |> List.map (\summary -> { id = summary.id, rating = summary.rating, version = summary.ratingVersion })


{-| An older detail read cannot regress a rating/version pair already
acknowledged by a save (or seen at a newer version).
-}
guardRatingDetail : Model -> String -> PostDetail -> PostDetail
guardRatingDetail model postId detail =
    case ( Ratings.knownVersion postId model.ratings, detail.ratingVersion ) of
        ( Just known, Just incoming ) ->
            if incoming < known then
                keepKnownRating model postId known detail

            else
                detail

        ( Just known, Nothing ) ->
            keepKnownRating model postId known detail

        _ ->
            detail


keepKnownRating : Model -> String -> Int -> PostDetail -> PostDetail
keepKnownRating model postId known detail =
    { detail
        | rating = Ratings.knownRating postId model.ratings |> Maybe.withDefault detail.rating
        , ratingVersion = Just known
        , ratingHistory =
            case Dict.get postId model.details of
                Just (DetailReady old) ->
                    old.ratingHistory

                _ ->
                    detail.ratingHistory
    }


ratingsContext : Model -> Ratings.Context
ratingsContext model =
    Ratings.context
        model.apiBase
        (App.Access.credential model.access)
        (App.Access.canWrite model.access)
        model.access.generation
        (ratingTargets model)


delegateRatings : Ratings.Msg -> Model -> ( Model, Cmd Msg )
delegateRatings subMsg model =
    let
        ( ratings, ratingsCmd, outs ) =
            Ratings.update (ratingsContext model) subMsg model.ratings
    in
    handleRatingsOuts outs ( { model | ratings = ratings }, Cmd.map RatingsMsg ratingsCmd )


handleRatingsOuts : List Ratings.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleRatingsOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleRatingsOuts rest (applyRatingsOut out ( model, cmd ))


applyRatingsOut : Ratings.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyRatingsOut out ( model, cmd ) =
    case out of
        Ratings.Saved response _ ->
            let
                effectivePost post =
                    case ( Ratings.knownVersion post.id model.ratings, Ratings.knownRating post.id model.ratings ) of
                        ( Just knownVersion_, Just knownRating_ ) ->
                            if knownVersion_ > post.version then
                                { post | version = knownVersion_, rating = knownRating_ }

                            else
                                post

                        _ ->
                            post

                effectivePosts =
                    List.map effectivePost response.posts

                sequence =
                    List.foldl
                        (\post sequence_ -> Sequence.updateSummary post.id (\summary -> { summary | rating = post.rating, ratingVersion = Just post.version }) sequence_)
                        model.sequence
                        effectivePosts

                details =
                    List.foldl
                        (\post dict ->
                            Dict.update post.id
                                (\state ->
                                    case state of
                                        Just (DetailReady detail) ->
                                            Just (DetailReady { detail | rating = post.rating, ratingVersion = Just post.version })

                                        other ->
                                            other
                                )
                                dict
                        )
                        model.details
                        effectivePosts
            in
            let
                ( refreshed, refreshCmd ) =
                    refreshQueryMembership { model | sequence = sequence, details = details }

                detailCmd =
                    case model.selection.active of
                        Just activeId ->
                            if List.any (\post -> post.id == activeId) response.posts then
                                Api.Post.detail model.apiBase activeId (DetailCompleted model.access.generation model.detailEpoch activeId)

                            else
                                Cmd.none

                        Nothing ->
                            Cmd.none
            in
            ( refreshed, Cmd.batch [ cmd, refreshCmd, detailCmd ] )

        Ratings.Uncertain postIds ->
            let
                ( refreshed, refreshCmd ) =
                    refreshPublicPosts postIds model
            in
            refreshQueryMembership refreshed |> withCmd (Cmd.batch [ cmd, refreshCmd ])

        Ratings.StaleResponse postIds ->
            let
                ( refreshed, refreshCmd ) =
                    refreshPublicPosts postIds model
            in
            refreshQueryMembership refreshed |> withCmd (Cmd.batch [ cmd, refreshCmd ])

        Ratings.AccessRevoked ->
            revokeAccess model |> withCmd cmd

        Ratings.ReloadTargets postIds ->
            refreshPublicPosts postIds model |> withCmd cmd


{-| After a rating change the current query (never a captured one) decides
membership again: browsed collections reload their bounded prefix and
ordinary search re-reads its loaded prefix, preserving selection/scroll
for posts that still match.
-}
refreshQueryMembership : Model -> ( Model, Cmd Msg )
refreshQueryMembership model =
    case Collections.browse model.collections of
        Just _ ->
            delegateCollections Collections.RetryRequested model

        Nothing ->
            if Search.state model.search == Search.Idle then
                ( model, Cmd.none )

            else
                let
                    ( search, searchCmd ) =
                        Search.refreshPrefix (searchContext model) (Sequence.length model.sequence) model.search
                in
                ( { model | search = search }, Cmd.map SearchMsg searchCmd )


prefixResultsArrived : SearchResponse -> Model -> ( Model, Cmd Msg )
prefixResultsArrived response current =
    let
        model =
            noteRatingSummaries (response.posts) current

        sequence =
            Sequence.fromList (effectiveRatingSummaries model response.posts)

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode

        base =
            { model
                | sequence = sequence
                , collections = Collections.abandonBrowse model.collections
                , selection = selection
                , mode = mode
                , pendingRoute = Nothing
                , uploadOrigin = Nothing
            }

        top =
            clampScroll base base.viewport.scrollTop

        updated =
            { base | viewport = setScroll top base.viewport }

        ( routed, routeCmd ) =
            case model.pendingRoute of
                Just route ->
                    applyRoute route updated

                Nothing ->
                    ( updated, Cmd.none )
    in
    ( routed
    , Cmd.batch [ restoreGridFocusAndScroll selection.active top, routeCmd ]
    )


{-| Per-call collections context: the current credential and write
state, the selection-derived add targets, and the loaded result count
that sizes prefix-refresh targets. The feature never stores any of it.
-}
collectionsContext : Model -> Collections.Context
collectionsContext model =
    Collections.context
        model.apiBase
        (App.Access.credential model.access)
        (App.Access.canWrite model.access)
        model.access.generation
        (Selection.targets model.sequence model.selection)
        (Sequence.length model.sequence)


trashContext : Model -> Trash.Context
trashContext model =
    Trash.context model.apiBase (App.Access.credential model.access) (App.Access.canWrite model.access) model.access.generation


delegateTrash : Trash.Msg -> Model -> ( Model, Cmd Msg )
delegateTrash subMsg model =
    let
        safeMsg =
            case subMsg of
                Trash.TrashConfirmRequested ->
                    case model.duplicateTrash of
                        Just ( hash, ids ) ->
                            if Duplicates.currentHash model.duplicates == Just hash
                                && Duplicates.isOpen model.duplicates
                                && not (Duplicates.membersLoading model.duplicates)
                                && List.sort ids == List.sort (Duplicates.trashableIds model.duplicates) then
                                subMsg

                            else
                                Trash.TrashCancelled

                        Nothing ->
                            case model.similarityTrash of
                                Just ( anchorId, ids, generation ) ->
                                    if Similarity.isOpen model.similarity
                                        && Similarity.anchorId model.similarity == Just anchorId
                                        && generation == model.access.generation
                                        && List.sort ids == List.sort (Similarity.trashableIds model.similarity) then
                                        subMsg

                                    else
                                        Trash.TrashCancelled

                                Nothing ->
                                    subMsg

                _ ->
                    subMsg

        ( trash, cmd, outs ) =
            Trash.update (trashContext model) safeMsg model.trash

        base =
            ( { model
                | trash = trash
                , duplicateTrash = if Trash.pendingTrashCount trash == Nothing then Nothing else model.duplicateTrash
                , similarityTrash = if Trash.pendingTrashCount trash == Nothing then Nothing else model.similarityTrash
              }
            , Cmd.map TrashMsg cmd
            )
    in
    List.foldl applyTrashOut base outs


applyTrashOut : Trash.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyTrashOut out ( model, cmd ) =
    case out of
        Trash.TrashApplied ids ->
            let
                ( reconciled, reconcileCmd ) =
                    reconcileAfterTrash False ids model

                ( synced, dupCmd ) =
                    syncDuplicatesAfterTrash False ids reconciled

                ( withSimilarity, simCmd ) =
                    syncSimilarityAfterTrash False ids synced
            in
            ( withSimilarity, Cmd.batch [ cmd, reconcileCmd, dupCmd, simCmd ] )

        Trash.RestoreApplied ids ->
            let
                ( reconciled, reconcileCmd ) =
                    reconcileAfterTrash True ids model

                ( synced, dupCmd ) =
                    syncDuplicatesAfterTrash True ids reconciled

                ( withSimilarity, simCmd ) =
                    syncSimilarityAfterTrash True ids synced
            in
            ( withSimilarity, Cmd.batch [ cmd, reconcileCmd, dupCmd, simCmd ] )

        Trash.AccessRevoked ->
            revokeAccess model |> withCmd cmd


sessionContext : Model -> Session.Context
sessionContext model =
    Session.context model.apiBase model.access.generation


sessionKnownPrivate : Model -> Bool
sessionKnownPrivate model =
    Session.privateMode model.session && model.session.session /= Nothing


tokenUnlocked : Model -> Bool
tokenUnlocked model =
    model.access.status == App.Access.Unlocked && model.access.token /= Nothing


canWriteEffective : Model -> Bool
canWriteEffective model =
    if sessionKnownPrivate model then
        Session.canWrite model.session || tokenUnlocked model

    else
        App.Access.canWrite model.access || Session.canWrite model.session


canModerateEffective : Model -> Bool
canModerateEffective model =
    if sessionKnownPrivate model then
        Session.canModerate model.session || tokenUnlocked model

    else
        App.Access.canWrite model.access || Session.canModerate model.session


isAdminEffective : Model -> Bool
isAdminEffective model =
    if sessionKnownPrivate model then
        Session.isAdmin model.session || tokenUnlocked model

    else
        App.Access.canWrite model.access || Session.isAdmin model.session


isSessionAuthenticated : Model -> Bool
isSessionAuthenticated model =
    model.session.session |> Maybe.map (\session -> session.authenticated && session.user /= Nothing) |> Maybe.withDefault False


sessionGateBlocking : Model -> Bool
sessionGateBlocking model =
    model.session.loading
        || (Session.privateMode model.session
                && not (Session.canRead model.session)
                && not (tokenUnlocked model)
           )


sessionGateHasError : Model -> Bool
sessionGateHasError model =
    not model.session.loading
        && model.session.session
        == Nothing
        && Session.privateMode model.session
        && Session.status model.session
        /= Nothing


delegateSession : Session.Msg -> Model -> ( Model, Cmd Msg )
delegateSession subMsg model =
    let
        ( session, cmd, outs ) =
            Session.update (sessionContext model) subMsg model.session

        ( next, nextCmd ) =
            handleSessionOuts outs ( { model | session = session }, Cmd.map SessionMsg cmd )

        ( importModel, importCmd, importOuts ) =
            BrowserImport.sync (importContext next) next.browserImport

        ( withImport, importCmds ) =
            handleImportOuts importOuts ( { next | browserImport = importModel }, Cmd.map ImportMsg importCmd )
    in
    ( withImport, Cmd.batch [ nextCmd, importCmds ] )


handleSessionOuts : List Session.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleSessionOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleSessionOuts rest (applySessionOut out ( model, cmd ))


applySessionOut : Session.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applySessionOut out ( model, cmd ) =
    case out of
        Session.SessionChanged ->
            ( model, Cmd.batch [ cmd, Nav.reload ] )

        Session.AccessRevoked ->
            accessRevokedNow model |> withCmd cmd


accessRevokedNow : Model -> ( Model, Cmd Msg )
accessRevokedNow model =
    let
        cleared =
            { model
                | access = App.Access.revoked model.access
                , session = Session.accessRevoked model.session
                , savedSearches = []
                , savedSearchFallback = True
                , savedSearchSaving = False
                , tags = Tags.locked model.tags
                , reactions = Reactions.locked model.reactions
                , ratings = Ratings.locked model.ratings
                , metadata = Metadata.locked model.metadata
                , bulkMetadata = BulkMetadata.locked model.bulkMetadata
                , collections = Collections.locked model.collections
                , trash = Trash.locked model.trash
                , duplicates = Duplicates.locked model.duplicates
                , moderation = Moderation.locked model.moderation
                , similarity = Similarity.locked model.similarity
                , recovery = Recovery.locked model.recovery
                , browserImport = BrowserImport.init
                , similarityTrash = Nothing
                , similarityCompare = Nothing
                , duplicateTrash = Nothing
                , tagManagement = TagManagement.locked model.tagManagement
                , tagSuggestions = TagSuggestions.init
                , details = Dict.empty
                , sequence = Sequence.empty
                , selection = Selection.empty
                , mode = Grid
                , uploadedPosts = Dict.empty
                , duplicatesOrigin = Nothing
                , uploadOrigin = Nothing
                , hint = Just "Your session expired. Sign in again to continue."
            }
    in
    ( cleared, Cmd.batch [ ModerationPreview.releaseAll, Nav.reload ] )


similarityContext : Model -> Similarity.Context
similarityContext model =
    Similarity.context model.apiBase (App.Access.credential model.access) (canWriteEffective model) model.access.generation


delegateSimilarity : Similarity.Msg -> Model -> ( Model, Cmd Msg )
delegateSimilarity subMsg model =
    let
        ( similarity, cmd, outs ) =
            Similarity.update (similarityContext model) subMsg model.similarity

        invalidated =
            case model.similarityTrash of
                Just ( anchorId, ids, generation ) ->
                    not (Similarity.isOpen similarity)
                        || Similarity.anchorId similarity
                        /= Just anchorId
                        || generation
                        /= model.access.generation
                        || List.sort ids
                        /= List.sort (Similarity.trashableIds similarity)

                Nothing ->
                    False

        safe =
            if invalidated && not model.trash.busy then
                { model | similarity = similarity, similarityTrash = Nothing, trash = cancelSimilarityTrash model }

            else
                { model | similarity = similarity }
    in
    handleSimilarityOuts outs ( safe, Cmd.map SimilarityMsg cmd )


cancelSimilarityTrash : Model -> Trash.Model
cancelSimilarityTrash model =
    let
        ( trash, _, _ ) =
            Trash.update (trashContext model) Trash.TrashCancelled model.trash
    in
    trash


invalidateSimilarityTrash : Model -> Model
invalidateSimilarityTrash model =
    case model.similarityTrash of
        Nothing ->
            model

        Just _ ->
            if model.trash.busy then
                { model | similarityTrash = Nothing }

            else
                { model | similarityTrash = Nothing, trash = cancelSimilarityTrash model }


handleSimilarityOuts : List Similarity.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleSimilarityOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleSimilarityOuts rest (applySimilarityOut out ( model, cmd ))


applySimilarityOut : Similarity.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applySimilarityOut out ( model, cmd ) =
    case out of
        Similarity.AccessRevoked ->
            accessRevokedNow model |> withCmd cmd

        Similarity.ResultUncertain _ ->
            ( model, cmd )

        Similarity.CompareCopies anchor candidate members ->
            if not (Similarity.isOpen model.similarity)
                || Similarity.anchorId model.similarity
                /= Just anchor.id
                || not (List.member candidate members)
                || not (List.member anchor members)
                || List.length members
                < 2
            then
                ( { model | hint = Just "That match is no longer loaded. Reload before comparing." }, cmd )

            else
                ( { model | similarityCompare = Just ( anchor.id, candidate.id ) }
                , Cmd.batch [ cmd, Task.attempt (SimilarityCompareMeasured anchor.id candidate.id) (Dom.getViewportOf MediaGrid.elementId) ]
                )

        Similarity.TrashSelected ids ->
            let
                valid =
                    List.filter (\postId -> List.member postId (Similarity.trashableIds model.similarity)) ids
            in
            case ( Similarity.anchorId model.similarity, valid ) of
                ( Just anchorId, _ :: _ ) ->
                    delegateTrash (Trash.TrashSelectedRequested valid)
                        { model | similarityTrash = Just ( anchorId, valid, model.access.generation ) }
                        |> withCmd cmd

                _ ->
                    ( model, cmd )


handleSimilarityCompareMeasured : String -> String -> Result Dom.Error Dom.Viewport -> Model -> ( Model, Cmd Msg )
handleSimilarityCompareMeasured anchorId candidateId result model =
    let
        members =
            case Similarity.anchor model.similarity of
                Just anchorSummary ->
                    anchorSummary :: Similarity.members model.similarity

                Nothing ->
                    []
    in
    if model.similarityCompare /= Just ( anchorId, candidateId )
        || not (Similarity.isOpen model.similarity)
        || Similarity.anchorId model.similarity
        /= Just anchorId
        || Similarity.isBusy model.similarity
        || not (List.all (\id -> List.member id (List.map .id members)) [ anchorId, candidateId ])
    then
        ( { model | similarityCompare = Nothing }, Cmd.none )

    else
        let
            ( opened, command ) =
                openDuplicatesCompare anchorId candidateId members { model | similarityCompare = Nothing }

            top =
                result |> Result.map (\viewport -> viewport.viewport.y) |> Result.withDefault model.viewport.scrollTop
        in
        ( { opened | returnPoint = Maybe.map (\point -> { point | scrollTop = top }) opened.returnPoint }, command )


syncSimilarityAfterTrash : Bool -> List String -> Model -> ( Model, Cmd Msg )
syncSimilarityAfterTrash isRestore ids model =
    let
        message =
            if isRestore then
                Similarity.RestoreApplied ids

            else
                Similarity.TrashApplied ids

        ( similarity, cmd, outs ) =
            Similarity.update (similarityContext model) message model.similarity
    in
    handleSimilarityOuts outs ( { model | similarity = similarity }, Cmd.map SimilarityMsg cmd )


syncSimilarityVisibility : Model -> ( Model, Cmd Msg )
syncSimilarityVisibility model =
    let
        ( similarity, cmd, outs ) =
            Similarity.update (similarityContext model) Similarity.VisibilityChanged model.similarity
    in
    handleSimilarityOuts outs ( { model | similarity = similarity }, Cmd.map SimilarityMsg cmd )


recoveryContext : Model -> Recovery.Context
recoveryContext model =
    Recovery.context model.apiBase (App.Access.credential model.access) (isAdminEffective model) model.access.generation


delegateRecovery : Recovery.Msg -> Model -> ( Model, Cmd Msg )
delegateRecovery subMsg model =
    let
        ( recovery, cmd, outs ) =
            Recovery.update (recoveryContext model) subMsg model.recovery

        apply out ( current, commands ) =
            case out of
                Recovery.AccessRevoked ->
                    accessRevokedNow current |> withCmd commands
    in
    List.foldl apply ( { model | recovery = recovery }, Cmd.map RecoveryMsg cmd ) outs


moderationContext : Model -> Moderation.Context
moderationContext model =
    Moderation.context model.apiBase (App.Access.credential model.access) (canModerateEffective model) model.access.generation


delegateModeration : Moderation.Msg -> Model -> ( Model, Cmd Msg )
delegateModeration subMsg model =
    let
        ( moderation, cmd, outs ) =
            Moderation.update (moderationContext model) subMsg model.moderation

        apply out ( current, commands ) =
            case out of
                Moderation.AccessRevoked ->
                    revokeAccess current |> withCmd commands

                Moderation.ModerationApplied applied ->
                    let
                        cleared =
                            { current | details = List.foldl Dict.remove current.details applied.ids, detailEpoch = current.detailEpoch + 1 }

                        ( reconciled, refresh ) =
                            reconcileAfterTrash (applied.newState == Domain.Moderation.Published) applied.ids cleared

                        ( duplicates, dupCmd ) =
                            syncDuplicatesAfterTrash (applied.newState == Domain.Moderation.Published) applied.ids reconciled

                        ( withSimilarity, simCmd ) =
                            syncSimilarityVisibility duplicates
                    in
                    ( withSimilarity, Cmd.batch [ commands, refresh, dupCmd, simCmd ] )
    in
    List.foldl apply ( { model | moderation = moderation }, Cmd.map ModerationMsg cmd ) outs


duplicatesContext : Model -> Duplicates.Context
duplicatesContext model =
    Duplicates.context model.apiBase (App.Access.credential model.access) (App.Access.canWrite model.access) model.access.generation


delegateDuplicates : Duplicates.Msg -> Model -> ( Model, Cmd Msg )
delegateDuplicates subMsg model =
    let
        ( duplicates, dupCmd, outs ) =
            Duplicates.update (duplicatesContext model) subMsg model.duplicates
        invalidated =
            case model.duplicateTrash of
                Just ( hash, ids ) ->
                    not (Duplicates.isOpen duplicates)
                        || Duplicates.currentHash duplicates /= Just hash
                        || Duplicates.membersLoading duplicates
                        || List.sort ids /= List.sort (Duplicates.trashableIds duplicates)

                Nothing ->
                    False

        safe =
            if invalidated && not model.trash.busy then
                { model | duplicates = duplicates, duplicateTrash = Nothing, trash = cancelDuplicateTrash model }

            else
                { model | duplicates = duplicates }
    in
    handleDuplicatesOuts outs ( safe, Cmd.map DuplicatesMsg dupCmd )


cancelDuplicateTrash : Model -> Trash.Model
cancelDuplicateTrash model =
    let
        ( trash, _, _ ) =
            Trash.update (trashContext model) Trash.TrashCancelled model.trash
    in
    trash


handleDuplicatesOuts : List Duplicates.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleDuplicatesOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleDuplicatesOuts rest (applyDuplicatesOut out ( model, cmd ))


applyDuplicatesOut : Duplicates.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyDuplicatesOut out ( model, cmd ) =
    case out of
        Duplicates.CompareCopies select candidate _ ->
            case Duplicates.currentHash model.duplicates of
                Just hash ->
                    ( model, Cmd.batch [ cmd, Task.attempt (DuplicateCompareMeasured hash select candidate) (Dom.getViewportOf MediaGrid.elementId) ] )

                Nothing ->
                    ( model, cmd )

        Duplicates.TrashSelected ids ->
            -- Re-validate at forwarding time: only ids still loaded in
            -- the open group may enter the existing Trash confirmation.
            let
                valid =
                    List.filter (\postId -> List.member postId (Duplicates.trashableIds model.duplicates)) ids
            in
            if List.isEmpty valid then
                ( model, cmd )

            else
                delegateTrash (Trash.TrashSelectedRequested valid) { model | duplicateTrash = Maybe.map (\hash -> ( hash, valid )) (Duplicates.currentHash model.duplicates) } |> withCmd cmd


{-| Bounded reconciliation of the duplicates panel after a confirmed
Trash/restore. Only the group list and the open group's first page are
re-read; the Library sequence is handled by `reconcileAfterTrash`.
-}
syncDuplicatesAfterTrash : Bool -> List String -> Model -> ( Model, Cmd Msg )
syncDuplicatesAfterTrash isRestore ids model =
    let
        message =
            if isRestore then
                Duplicates.RestoreApplied ids

            else
                Duplicates.TrashApplied ids

        ( duplicates, dupCmd, _ ) =
            Duplicates.update (duplicatesContext model) message model.duplicates
    in
    ( { model | duplicates = duplicates }, Cmd.map DuplicatesMsg dupCmd )


{-| Open the regular Compare mode on a temporary member sequence.
The Library's query, collection, loaded prefix, selection, and grid
scroll are measured before entering Compare and
restored by `exitToGrid`; the grid selection itself is untouched
because the temporary selection is discarded on return.
-}
openDuplicatesCompare : String -> String -> List PostSummary -> Model -> ( Model, Cmd Msg )
openDuplicatesCompare select candidate members model =
    if model.mode /= Grid then
        ( { model | hint = Just "Return to the grid before comparing copies." }, Cmd.none )

    else if List.length members < 2 then
        ( { model | hint = Just "Compare needs at least two copies." }, Cmd.none )

    else
        let
            origin =
                case model.duplicatesOrigin of
                    Just saved ->
                        saved

                    Nothing ->
                        { sequence = model.sequence, selection = model.selection }

            tempSelection =
                Selection.click select Selection.empty |> Selection.toggle candidate

            withTemp =
                { model
                    | sequence = Sequence.fromList members
                    , selection = tempSelection
                    , duplicatesOrigin = Just origin
                }
        in
        let
            ( opened, command ) =
                enterMode (Compare { select = select, candidate = candidate }) withTemp
        in
        ( { opened | returnPoint = Maybe.map (\point -> { point | activeAtEntry = origin.selection.active }) opened.returnPoint }, command )


pruneOrigin : List String -> UploadOrigin -> UploadOrigin
pruneOrigin ids origin =
    let
        sequence =
            Sequence.fromList
                (Sequence.slice 0 (Sequence.length origin.sequence) origin.sequence
                    |> List.map Tuple.second
                    |> List.filter (\post -> not (List.member post.id ids))
                )
    in
    { sequence = sequence, selection = Selection.prune sequence origin.selection }


tagManagementContext : Model -> TagManagement.Context
tagManagementContext model =
    TagManagement.context model.apiBase (App.Access.credential model.access) (App.Access.canWrite model.access) model.access.generation


delegateTagManagement : TagManagement.Msg -> Model -> ( Model, Cmd Msg )
delegateTagManagement subMsg model =
    let
        ( tm, cmd, outs ) =
            TagManagement.update (tagManagementContext model) subMsg model.tagManagement

        base =
            ( { model | tagManagement = tm }, Cmd.map TagManagementMsg cmd )
    in
    List.foldl applyTagManagementOut base outs


applyTagManagementOut : TagManagement.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyTagManagementOut out ( model, cmd ) =
    case out of
        TagManagement.Applied _ ->
            reconcileAfterTagManagement model |> withCmd cmd

        TagManagement.ResultUncertain ->
            reconcileAfterTagManagement model |> withCmd cmd

        TagManagement.AccessRevoked ->
            revokeAccess model |> withCmd cmd


{-| After a global rename/merge, and likewise when an apply result is
uncertain and may have changed tags on the server: cached details and
tag versions are stale, tag drafts/confirmations and suggestions
clear, the loaded search or collection prefix re-reads without
changing query/selection/scroll, and the active detail reloads under
a new detail epoch so prior in-flight detail responses cannot
reintroduce old tags. An uncertain result is never retried here; the
feature still blocks resubmission until a fresh preview.
-}
reconcileAfterTagManagement : Model -> ( Model, Cmd Msg )
reconcileAfterTagManagement model =
    let
        epoch =
            model.detailEpoch + 1

        cleared =
            { model
                | details = Dict.empty
                , detailEpoch = epoch
                , tags = Tags.globalTagsChanged model.tags
                , tagSuggestions = TagSuggestions.clear model.tagSuggestions
            }

        ( refreshed, refreshCmd ) =
            refreshQueryMembership cleared

        detailCmd =
            case refreshed.selection.active of
                Just postId ->
                    Api.Post.detail refreshed.apiBase postId (DetailCompleted refreshed.access.generation epoch postId)

                Nothing ->
                    Cmd.none

        withDetail =
            case refreshed.selection.active of
                Just postId ->
                    { refreshed | details = Dict.insert postId DetailLoading refreshed.details }

                Nothing ->
                    refreshed
    in
    ( withDetail, Cmd.batch [ refreshCmd, detailCmd ] )


{-| After a durable trash/restore: browsed collections reload their
bounded prefix; ordinary search drops trashed ids deliberately, prunes
selection, then refreshes survivors from the server (no retry of the
mutation itself).
-}
reconcileAfterTrash : Bool -> List String -> Model -> ( Model, Cmd Msg )
reconcileAfterTrash isRestore ids model0 =
    let
        -- A stale temporary origin must never restore trashed ids.
        -- Restores do not prune origins: a restored post may rejoin.
        model =
            if isRestore then
                model0

            else
                { model0
                    | uploadOrigin = Maybe.map (pruneOrigin ids) model0.uploadOrigin
                    , duplicatesOrigin = Maybe.map (pruneOrigin ids) model0.duplicatesOrigin
                }
    in
    case model.duplicatesOrigin of
        Just _ ->
            -- Inside a duplicates Compare/Loupe sequence: prune it in
            -- place and keep (or exit) the mode instead of reloading
            -- the whole Library query.
            let
                sequence =
                    Sequence.fromList
                        (Sequence.slice 0 (Sequence.length model.sequence) model.sequence
                            |> List.map Tuple.second
                            |> List.filter (\post -> not (List.member post.id ids) || isRestore)
                        )

                selection =
                    Selection.prune sequence model.selection

                pruned =
                    { model | sequence = sequence, selection = selection }

                mode =
                    validMode sequence selection model.mode
            in
            if mode == Grid && model.mode /= Grid then
                exitToGrid pruned

            else
                ( { pruned | mode = mode }, Cmd.none )

        Nothing ->
            case Collections.browse model.collections of
                Just _ ->
                    delegateCollections Collections.RetryRequested model

                Nothing ->
                    let
                        survivors =
                            Sequence.fromList (List.filter (\p -> isRestore || not (List.member p.id ids)) (Sequence.slice 0 (Sequence.length model.sequence) model.sequence |> List.map Tuple.second))

                        selection =
                            Selection.prune survivors model.selection

                        pruned =
                            { model | sequence = survivors, selection = selection, mode = validMode survivors selection model.mode }
                    in
                    refreshQueryMembership pruned


delegateCollections : Collections.Msg -> Model -> ( Model, Cmd Msg )
delegateCollections subMsg model =
    let
        ( collections, collectionsCmd, outs ) =
            Collections.update (collectionsContext model) subMsg model.collections
    in
    handleCollectionsOuts outs ( { model | collections = collections }, Cmd.map CollectionsMsg collectionsCmd )


handleCollectionsOuts : List Collections.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
handleCollectionsOuts outs ( model, cmd ) =
    case outs of
        [] ->
            ( model, cmd )

        out :: rest ->
            handleCollectionsOuts rest (applyCollectionsOut out ( model, cmd ))


{-| Collection arrivals merge posts into the workspace while panels,
mode, and scroll behavior stay with the page. A fresh browse replaces
the result set from the top; appends extend it in place; a refresh
swaps in the assembled prefix with scroll clamped to the current
offset.
-}
applyCollectionsOut : Collections.OutMsg -> ( Model, Cmd Msg ) -> ( Model, Cmd Msg )
applyCollectionsOut out ( model, cmd ) =
    case out of
        Collections.BrowseLoaded arrival ->
            collectionBrowseLoaded arrival model |> withCmd cmd

        Collections.AppendLoaded arrival ->
            collectionAppendLoaded arrival model |> withCmd cmd

        Collections.RefreshLoaded arrival ->
            collectionRefreshLoaded arrival model |> withCmd cmd

        Collections.BrowseEnded ->
            ( { model
                | sequence = Sequence.empty
                , selection = Selection.empty
                , mode = Grid
                , pendingRoute = Nothing
                , uploadOrigin = Nothing
                , viewport = setScroll 0 model.viewport
              }
            , Cmd.none
            )
                |> withCmd cmd

        Collections.AccessRevoked ->
            revokeAccess model |> withCmd cmd


collectionBrowseLoaded : { posts : List PostSummary, nextCursor : Maybe String } -> Model -> ( Model, Cmd Msg )
collectionBrowseLoaded arrival current =
    let
        model =
            noteRatingSummaries (arrival.posts) current

        sequence =
            Sequence.fromList (effectiveRatingSummaries model arrival.posts)

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode
    in
    ( { model
        | sequence = sequence
        , selection = selection
        , mode = mode
        , pendingRoute = Nothing
        , viewport = setScroll 0 model.viewport
        , uploadOrigin = Nothing
      }
    , scrollGridLater 0
    )


collectionAppendLoaded : { posts : List PostSummary, nextCursor : Maybe String } -> Model -> ( Model, Cmd Msg )
collectionAppendLoaded arrival current =
    let
        model =
            noteRatingSummaries (arrival.posts) current

        sequence =
            Sequence.append (effectiveRatingSummaries model arrival.posts) model.sequence

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode
    in
    ( { model
        | sequence = sequence
        , selection = selection
        , mode = mode
      }
    , Cmd.none
    )


collectionRefreshLoaded : { sequence : Sequence, nextCursor : Maybe String } -> Model -> ( Model, Cmd Msg )
collectionRefreshLoaded arrival current =
    let
        model =
            noteRatingSummaries (Sequence.slice 0 (Sequence.length arrival.sequence) arrival.sequence |> List.map Tuple.second) current

        guardedSequence =
            Sequence.slice 0 (Sequence.length arrival.sequence) arrival.sequence
                |> List.map Tuple.second
                |> effectiveRatingSummaries model
                |> Sequence.fromList

        selection =
            CollectionLogic.refreshSelection model.sequence guardedSequence model.selection

        mode =
            validMode guardedSequence selection model.mode

        base =
            { model
                | sequence = guardedSequence
                , selection = selection
                , mode = mode
                , pendingRoute = Nothing
                , uploadOrigin = Nothing
            }

        top =
            clampScroll base base.viewport.scrollTop

        updated =
            { base | viewport = setScroll top base.viewport }
    in
    ( updated
    , restoreGridFocusAndScroll selection.active top
    )



clearResults : Model -> Model
clearResults model =
    { model
        | draftQuery = ""
        , search = Search.reset model.search
        , details = Dict.empty
        , sequence = Sequence.empty
        , selection = Selection.empty
        , mode = Grid
        , returnPoint = Nothing
        , viewport = setScroll 0 model.viewport
        , pendingRoute = Nothing
        , collections = Collections.abandonBrowse model.collections
    }


{-| New results keep the workspace: panels, thumbnail size, and mode stay; the
active post and selection survive when their posts are still present.
-}
resultsArrived : SearchResponse -> Model -> ( Model, Cmd Msg )
resultsArrived response current =
    let
        model =
            noteRatingSummaries (response.posts) current

        posts =
            effectiveRatingSummaries model response.posts

        sequence =
            Sequence.fromList posts

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode

        base =
            { model
                | sequence = sequence
                , collections = Collections.abandonBrowse model.collections
                , selection = selection
                , mode = mode
                , recent = remember (Search.query model.search) model.recent
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




moreResultsArrived : SearchResponse -> Model -> ( Model, Cmd Msg )
moreResultsArrived response current =
    let
        model =
            noteRatingSummaries (response.posts) current

        sequence =
            Sequence.append (effectiveRatingSummaries model response.posts) model.sequence

        selection =
            Selection.prune sequence model.selection

        mode =
            validMode sequence selection model.mode
    in
    -- Cursor and loading state already settled in the feature before the
    -- arrival was reported; only the workspace merges here.
    ( { model
        | sequence = sequence
        , collections = Collections.abandonBrowse model.collections
        , selection = selection
        , mode = mode
      }
    , Cmd.none
    )


workspaceBusy : Model -> Bool
workspaceBusy model =
    case Collections.browse model.collections of
        Just _ ->
            Collections.isBusy model.collections

        Nothing ->
            Search.isBusy model.search


shouldLoadMore : Model -> Bool
shouldLoadMore model =
    case model.mode of
        Grid ->
            let
                hasMore =
                    case Collections.browse model.collections of
                        Just _ ->
                            Collections.hasMore model.collections

                        Nothing ->
                            Search.hasMore model.search
            in
            if hasMore then
                let
                    g =
                        geometry model

                    contentBottom =
                        Layout.totalHeight g (Sequence.length model.sequence)

                    threshold =
                        max 240 model.viewport.height
                in
                model.viewport.scrollTop + model.viewport.height >= contentBottom - threshold

            else
                False

        _ ->
            False


loadMore : Model -> ( Model, Cmd Msg )
loadMore model =
    -- Loading dispatches by result source: a browsed collection appends
    -- through the collections feature, while ordinary search paginates
    -- through the search feature.
    case Collections.browse model.collections of
        Just _ ->
            let
                ( collections, collectionsCmd ) =
                    Collections.requestMore (collectionsContext model) model.collections
            in
            ( { model | collections = collections }, Cmd.map CollectionsMsg collectionsCmd )

        Nothing ->
            let
                ( search, searchCmd ) =
                    Search.requestMore (searchContext model) model.search
            in
            ( { model | search = search }, Cmd.map SearchMsg searchCmd )


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
            { query = Search.query model.search, post = Just postId, view = LoupeView }

        _ ->
            { query = Search.query model.search, post = Nothing, view = GridView }


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
            case model.duplicatesOrigin of
                Just origin ->
                    { model
                        | sequence = origin.sequence
                        , selection = Selection.prune origin.sequence origin.selection
                        , duplicatesOrigin = Nothing
                        , uploadOrigin = Nothing
                    }

                Nothing ->
                    case model.uploadOrigin of
                        Just origin ->
                            { model
                                | sequence = origin.sequence
                                , selection = Selection.prune origin.sequence origin.selection
                                , uploadOrigin = Nothing
                            }

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
        [ Task.attempt (\_ -> NoOp)
            (if model.duplicatesOrigin /= Nothing then
                -- Wait for the restored panel and grid geometry to settle.
                Process.sleep 40 |> Task.andThen (\_ -> Dom.setViewportOf MediaGrid.elementId 0 top)

             else
                Dom.setViewportOf MediaGrid.elementId 0 top
            )
        , if model.duplicatesOrigin /= Nothing then
            -- Focusing an offscreen active cell would undo the saved scroll.
            Cmd.none

          else
            case restored.selection.active of
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
    let
        prepared =
            let
                resetMetadata =
                    Metadata.activeChanged model.metadata
            in
            { model
                | tags = Tags.activeChanged model.tags
                , tagSuggestions = TagSuggestions.clear model.tagSuggestions
                , reactions = Reactions.activeChanged (activeDetail model) model.reactions
                , metadata =
                    case activeDetail model of
                        Just detail ->
                            if model.selection.active == Just detail.id then
                                Metadata.syncDetail detail resetMetadata

                            else
                                resetMetadata

                        Nothing ->
                            resetMetadata
            }
    in
    case prepared.selection.active of
        Nothing ->
            ( prepared, Cmd.none )

        Just postId ->
            case Dict.get postId prepared.details of
                Just (DetailLoading) ->
                    ( prepared, Cmd.none )

                Just (DetailReady _) ->
                    ( prepared, Cmd.none )

                _ ->
                    ( { prepared | details = Dict.insert postId DetailLoading prepared.details }
                    , Api.Post.detail prepared.apiBase postId (DetailCompleted prepared.access.generation prepared.detailEpoch postId)
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
    -- Bounds come from the model's current sequence and layout, so a
    -- refresh that already swapped in the rebuilt sequence clamps
    -- against the new content, never the old.
    CollectionLogic.clampScrollTop
        (Layout.totalHeight (geometry model) (Sequence.length model.sequence))
        model.viewport.height
        top


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

                ( importModel, importCmd, importOuts ) =
                    BrowserImport.resume (importContext withQueue) withQueue.browserImport

                ( withImport, importCmds ) =
                    handleImportOuts importOuts ( { withQueue | browserImport = importModel }, Cmd.map ImportMsg importCmd )
            in
            ( { withImport | hint = Just "Unlocked. Writes enabled." }
            , Cmd.batch [ queueCmds, importCmds, loadServerSearches model ]
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
            , tags = Tags.locked model.tags
            , reactions = Reactions.locked model.reactions
            , ratings = Ratings.locked model.ratings
            , metadata = Metadata.locked model.metadata
            , bulkMetadata = BulkMetadata.locked model.bulkMetadata
            , collections = Collections.locked model.collections
            , trash = Trash.locked model.trash
            , duplicates = Duplicates.locked model.duplicates
            , moderation = Moderation.locked model.moderation
            , similarity = Similarity.locked model.similarity
            , recovery = Recovery.locked model.recovery
            , browserImport = BrowserImport.init
            , similarityTrash = Nothing
            , similarityCompare = Nothing
            , duplicateTrash = Nothing
            , tagManagement = TagManagement.locked model.tagManagement
            , tagSuggestions = TagSuggestions.init
            , hint = Just hint
        }
        |> withCmd ModerationPreview.releaseAll


{-| Server-initiated lock after a 401 on a credentialed request. Same
clearing as a manual lock, plus the fixed notice, then public
reconciliation. Stale in-flight flags reset because their completions
will arrive with an obsolete generation and be ignored.
-}
revokeAccess : Model -> ( Model, Cmd Msg )
revokeAccess model =
    if Session.privateMode model.session then
        accessRevokedNow model

    else
        reconcilePublic
            { model
                | access = App.Access.revoked model.access
                , savedSearches = []
                , savedSearchFallback = True
                , savedSearchSaving = False
                , tags = Tags.locked model.tags
                , reactions = Reactions.locked model.reactions
                , ratings = Ratings.locked model.ratings
                , metadata = Metadata.locked model.metadata
                , bulkMetadata = BulkMetadata.locked model.bulkMetadata
                , collections = Collections.locked model.collections
                , trash = Trash.locked model.trash
                , duplicates = Duplicates.locked model.duplicates
                , moderation = Moderation.locked model.moderation
                , similarity = Similarity.locked model.similarity
                , recovery = Recovery.locked model.recovery
                , browserImport = BrowserImport.init
                , similarityTrash = Nothing
                , similarityCompare = Nothing
                , duplicateTrash = Nothing
                , tagManagement = TagManagement.locked model.tagManagement
                , tagSuggestions = TagSuggestions.init
                , hint = Just "The browser was locked (401). Unlock to continue writing."
            }
            |> withCmd ModerationPreview.releaseAll


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

        ( collections, summariesCmd ) =
            Collections.requestSummaries (collectionsContext refreshed) refreshed.collections
    in
    ( { refreshed | collections = collections }
    , Cmd.batch [ postCmd, Cmd.map CollectionsMsg summariesCmd ]
    )


refreshPublicPosts : List String -> Model -> ( Model, Cmd Msg )
refreshPublicPosts postIds model =
    let
        ids =
            Set.fromList postIds |> Set.toList
    in
    ( { model | details = List.foldl (\id details -> Dict.insert id DetailLoading details) model.details ids }
    , ids |> List.map (\id -> Api.Post.detail model.apiBase id (DetailCompleted model.access.generation model.detailEpoch id)) |> Cmd.batch
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
            String.trim (Search.query model.search)
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
    if model.windowWidth >= 1440 && model.mode == Grid then
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


{-| Focusing an off-screen cell scrolls it into view. Restore focus before
the intended offset, then measure the final viewport instead of racing
independent focus and scroll commands.
-}
restoreGridFocusAndScroll : Maybe String -> Float -> Cmd Msg
restoreGridFocusAndScroll active top =
    Process.sleep 40
        |> Task.andThen
            (\_ ->
                case active of
                    Just postId ->
                        focusWithRetry (MediaGrid.cellId postId)
                            |> Task.onError (\_ -> Task.succeed ())

                    Nothing ->
                        Task.succeed ()
            )
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
    case ApiError.classify error of
        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The search timed out."

        ApiError.NetworkError ->
            "The search server could not be reached."

        ApiError.BadStatus status ->
            "Search failed (HTTP " ++ String.fromInt status ++ ")."

        ApiError.BadBody ->
            "The search response was invalid."









-- KEYBOARD


commandFor : Model -> KeyEvent -> Maybe Command
commandFor model event =
    if model.shortcutsOpen then
        if event.key == "Escape" || event.key == "?" then
            Just CloseShortcuts

        else
            Nothing

    else if event.target == Keyboard.MediaControl then
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
                        Just FocusTags

                    "b" ->
                        Just AddSelectionToCollection

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
        , Sub.map ImportMsg (BrowserImport.subscriptions model.browserImport)
        , ModerationPreview.previewEvent (Moderation.PreviewEventReceived >> ModerationMsg)
        ]



-- VIEW


view : Model -> Browser.Document Msg
view model =
    { title = documentTitle model
    , body = [ shell model ]
    }


documentTitle : Model -> String
documentTitle model =
    case Collections.activeName model.collections of
        Just name ->
            name ++ " · Kura"

        Nothing ->
            if String.trim (Search.query model.search) == "" then
                "Kura"

            else
                case ( model.mode, model.selection.active ) of
                    ( Loupe, Just postId ) ->
                        "#" ++ postId ++ " · " ++ Search.query model.search ++ " · Kura"

                    _ ->
                        Search.query model.search ++ " · Kura"


sessionGateView : Model -> Html Msg
sessionGateView model =
    div [ class "session-gate-wrap" ]
        ([ case Session.viewGate model.session of
            Just gate ->
                Html.map SessionMsg gate

            Nothing ->
                text ""
         ]
            ++ (if sessionGateHasError model then
                    [ p [ class "panel-note" ] [ text "Access could not be checked. " ]
                    , button [ class "button button-quiet", type_ "button", onClick (SessionMsg Session.RefreshRequested) ] [ text "Retry access check" ]
                    ]

                else
                    []
               )
            ++ (if sessionGateBlocking model && not model.session.loading && not (Session.privateMode model.session) then
                    [ button [ class "button button-quiet", type_ "button", onClick (SessionMsg Session.RefreshRequested) ] [ text "Retry" ] ]

                else
                    []
               )
        )


shell : Model -> Html Msg
shell model =
    div [ class "kura-shell", classList [ ( "is-viewing", model.mode /= Grid ) ], preventDefaultOn "keydown" (keyDecoder model) ]
        [ topBar model
        , div [ class "progress", classList [ ( "is-active", workspaceBusy model ) ] ] []
        , if sessionGateBlocking model then
            sessionGateView model

          else
            div [ class "kura-body" ]
            [ navigatorView model
            , div [ class "workspace" ]
                [ if model.prefs.filterBar && model.mode == Grid then
                    QueryEditor.filterBar
                        { query = if Collections.browse model.collections == Nothing then Search.query model.search else ""
                        , thumb = model.prefs.thumbSize
                        , extras = model.prefs.cellExtras
                        , onRemoveTerm = TermRemoved
                        , onThumb = ThumbChanged
                        , onCycleExtras = KeyCommand CycleExtras
                        , onOrder = OrderChanged
                        , collectionName = Collections.activeName model.collections
                        }

                  else
                    text ""
                , libraryTools model
                , stage model
                ]
            , inspectorView model
            , Html.map UploadMsg (UploadQueue.view (App.Access.canWrite model.access) model.upload)
            , Html.map ImportMsg (BrowserImport.view (App.Access.canWrite model.access) (Collections.items model.collections) model.browserImport)
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
        [ h1 [ class "wordmark" ]
            [ button
                [ class "brand-button"
                , type_ "button"
                , onClick (KeyCommand ToggleNavigator)
                , attribute "aria-label" "Kura library"
                , attribute "aria-expanded" (if navigatorVisible model then "true" else "false")
                , title "Toggle library (Shift+N)"
                ]
                [ Ui.Icon.view "shelf", text "Kura" ]
            ]
        , QueryEditor.field { draft = model.draftQuery, onInput = DraftChanged, onSubmit = SearchSubmitted }
        , div [ class "mode-switch", attribute "role" "group", attribute "aria-label" "View mode" ]
            [ modeButton model "Grid" "G" BackToGrid (model.mode == Grid)
            , modeButton model "Loupe" "E" OpenLoupe (model.mode == Loupe)
            , modeButton model "Compare" "C" OpenCompare (isCompare model.mode)
            , modeButton model "Survey" "N" OpenSurvey (isSurvey model.mode)
            ]
        , div [ class "topbar-actions" ]
            [ uploadButton model
            , importButton model
            , themeButton model
            , accessControls model
            , Ui.Button.icon "info" [ class "button-quiet inspector-toggle" ]
                { label = "Toggle image details"
                , key = Nothing
                , onPress = Just (KeyCommand ToggleInspector)
                , pressed = Just (inspectorVisible model)
                , hint = Just "Toggle image details (I)"
                }
            ]
        ]


themeButton : Model -> Html Msg
themeButton model =
    let
        label =
            if model.prefs.darkMode then
                "Switch to light mode"

            else
                "Switch to dark mode"
    in
    Ui.Button.icon (if model.prefs.darkMode then "sun" else "moon") [ class "button-quiet theme-toggle" ]
        { label = label
        , key = Nothing
        , onPress = Just ThemeToggled
        , pressed = Just model.prefs.darkMode
        , hint = Just label
        }


importButton : Model -> Html Msg
importButton model =
    Ui.Button.view [ class "upload-button" ]
        { label = "Import folder"
        , key = Nothing
        , onPress =
            Just
                (ImportMsg
                    (if BrowserImport.isOpen model.browserImport then
                        BrowserImport.ClosePanel

                     else
                        BrowserImport.OpenPanel
                    )
                )
        , pressed = Just (BrowserImport.isOpen model.browserImport)
        , hint = Just "Import a folder of images"
        }


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
    Ui.Button.view [ class "upload-button" ]
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
    if isSessionAuthenticated model then
        span [ class "access-state", title "Signed in with a library account. Account controls are in Library tools." ] [ text "Signed in" ]

    else
        case model.access.status of
        App.Access.Checking ->
            span [ class "access-state" ] [ text "Checking access…" ]

        App.Access.OpenLocal ->
            span [ class "access-state is-local", title "Local mode: writes need no token." ] [ span [ class "access-dot", attribute "aria-hidden" "true" ] [], text "Local" ]

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
        , key = Nothing
        , onPress =
            if Sequence.isEmpty model.sequence then
                Nothing

            else
                Just (KeyCommand command)
        , pressed = Just active
        , hint = Just (label ++ " (" ++ key ++ ")")
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


navigatorManage : Model -> Maybe (Feature.Navigator.ManageConfig Msg)
navigatorManage model =
    Collections.manageView model.collections
        |> Maybe.map
            (\manage ->
                { collectionId = manage.collectionId
                , name = manage.name
                , rows =
                    List.map
                        (\row ->
                            { id = row.id
                            , canUp = row.canUp
                            , canDown = row.canDown
                            , canFirst = row.canFirst
                            , canLast = row.canLast
                            }
                        )
                        manage.rows
                , page = manage.page
                , pageCount = manage.pageCount
                , hasPrev = manage.hasPrev
                , hasNext = manage.hasNext
                , loading = manage.loading
                , busy = manage.busy
                , onMove = \postId delta -> CollectionsMsg (Collections.MoveRequested postId delta)
                , onMoveFirst = CollectionsMsg << Collections.MoveFirstRequested
                , onMoveLast = CollectionsMsg << Collections.MoveLastRequested
                , onRemove = CollectionsMsg << Collections.RemoveRequested
                , onPrev = CollectionsMsg Collections.ManageBackRequested
                , onNext = CollectionsMsg Collections.ManageNextRequested
                , onClose = CollectionsMsg Collections.ManageCloseRequested
                }
            )


libraryTools : Model -> Html Msg
libraryTools model =
    let
        selectedIds =
            Selection.targets model.sequence model.selection

        trashIds =
            if model.duplicatesOrigin /= Nothing then
                Duplicates.trashableIds model.duplicates

            else
                selectedIds

        selectionExport =
            Export.selectionUrl model.apiBase selectedIds model.exportMetadata

        activeCollection =
            Collections.activeId model.collections |> Maybe.andThen (\id -> Collections.items model.collections |> List.filter (\c -> c.id == id) |> List.head)

        collectionExport =
            activeCollection |> Maybe.andThen (\c -> Export.collectionUrl model.apiBase c.id (Just c.version) model.exportMetadata)
    in
    div [ class "panel-section library-tools" ]
        ([ div [ class "library-tools-row" ]
            [ button [ class "button button-quiet", type_ "button", onClick (TrashMsg Trash.OpenRequested), disabled (not (App.Access.canWrite model.access)) ] [ text "Trash" ]
            , button [ class "button button-quiet", type_ "button", onClick (DuplicatesMsg (if Duplicates.isOpen model.duplicates then Duplicates.CloseRequested else Duplicates.OpenRequested)) ] [ text "Exact copies" ]
            , (case activePost model of
                Just summary ->
                    button [ class "button button-quiet", type_ "button", onClick (SimilarityMsg (Similarity.OpenRequested summary)), disabled (Domain.Post.isVideoMedia summary.mediaType), title "Find visually similar images; video matching is unavailable" ] [ text "Find similar" ]

                Nothing ->
                    button [ class "button button-quiet", type_ "button", disabled True, title "Select a post to find similar posts" ] [ text "Find similar" ]
              )
            , button [ class "button button-quiet", type_ "button", onClick (ModerationMsg Moderation.OpenRequested), disabled (not (canModerateEffective model)) ] [ text "Moderation" ]
            , button [ class "button button-quiet", type_ "button", onClick (TagManagementMsg TagManagement.OpenRequested), disabled (not (App.Access.canWrite model.access)) ] [ text "Manage tags" ]
            , button [ class "button button-quiet", type_ "button", onClick (TrashMsg (Trash.TrashSelectedRequested trashIds)), disabled (not (App.Access.canWrite model.access) || List.isEmpty trashIds) ] [ text "Move selected to Trash" ]
            , Html.label [] [ Html.input [ type_ "checkbox", Html.Attributes.checked model.exportMetadata, Html.Events.onCheck ExportMetadataChanged ] [], text " Include metadata" ]
            , case selectionExport of
                Just url ->
                    Html.a [ Html.Attributes.href url, Html.Attributes.download "", class "button button-quiet" ] [ text "Export selection (ZIP)" ]

                Nothing ->
                    button [ class "button button-quiet", type_ "button", disabled True, title "Select 1–1000 posts to export" ] [ text "Export selection (ZIP)" ]
            , case collectionExport of
                Just url ->
                    Html.a [ Html.Attributes.href url, Html.Attributes.download "", class "button button-quiet" ] [ text ("Export " ++ (activeCollection |> Maybe.map .name |> Maybe.withDefault "collection") ++ " (ZIP)") ]

                Nothing ->
                    button [ class "button button-quiet", type_ "button", disabled True, title "Select a collection with a known version to export" ] [ text "Export collection (ZIP)" ]
            ]
         , p [ class "panel-note" ] [ text "Export up to 1,000 posts or 250 MiB of originals as ZIP." ]
         ]
            ++ (if Trash.isOpen model.trash || Trash.pendingTrashCount model.trash /= Nothing then
                    [ Html.map TrashMsg
                        (Trash.view model.trash
                            { onClose = Trash.CloseRequested
                            , onRestore = Trash.RestoreRequested
                            , onLoadMore = Trash.LoadMoreRequested
                            , onConfirmTrash = Trash.TrashConfirmRequested
                            , onCancelTrash = Trash.TrashCancelled
                            }
                        )
                    ]

                else
                    []
               )
            ++ (if Moderation.isOpen model.moderation then
                    [ Html.map ModerationMsg (Moderation.view model.moderation) ]

                else
                    []
               )
            ++ (if model.mode == Grid && Duplicates.isOpen model.duplicates then
                    [ Html.map DuplicatesMsg
                        (Duplicates.view model.duplicates
                            { apiBase = model.apiBase
                            , writesEnabled = App.Access.canWrite model.access
                            , onClose = Duplicates.CloseRequested
                            , onBack = Duplicates.BackRequested
                            , onReloadGroups = Duplicates.GroupsReloadRequested
                            , onLoadMoreGroups = Duplicates.GroupsLoadMoreRequested
                            , onOpenGroup = Duplicates.GroupOpenRequested
                            , onReloadMembers = Duplicates.MembersReloadRequested
                            , onLoadMoreMembers = Duplicates.MembersLoadMoreRequested
                            , onToggle = Duplicates.MemberToggled
                            , onClearSelection = Duplicates.SelectionCleared
                            , onCompare = Duplicates.CompareRequested
                            , onTrash = Duplicates.TrashRequested
                            , onMediaError = Duplicates.MediaFailed
                            }
                        )
                    ]

                else
                    []
               )
            ++ (if TagManagement.isOpen model.tagManagement then
                    [ Html.map TagManagementMsg
                        (TagManagement.view model.tagManagement
                            { onClose = TagManagement.CloseRequested
                            , onPrefix = TagManagement.PrefixChanged
                            , onLoadMore = TagManagement.LoadMoreRequested
                            , onChoose = TagManagement.SourceChosen
                            , onSource = TagManagement.SourceChanged
                            , onTarget = TagManagement.TargetChanged
                            , onMode = TagManagement.ModeChanged
                            , onPreview = TagManagement.PreviewRequested
                            , onApply = TagManagement.ApplyRequested
                            }
                        )
                    ]

                else
                    []
               )
            ++ (if isSessionAuthenticated model then
                    [ Html.map SessionMsg (Session.viewAccount (sessionContext model) model.session) ]

                else
                    []
               )
            ++ (if model.mode == Grid && Similarity.isOpen model.similarity then
                    [ Similarity.view model.similarity
                        { apiBase = model.apiBase
                        , writesEnabled = canWriteEffective model
                        , onClose = SimilarityMsg Similarity.CloseRequested
                        , onReload = SimilarityMsg Similarity.ReloadRequested
                        , onLoadMore = SimilarityMsg Similarity.LoadMoreRequested
                        , onThresholdChanged = \raw -> SimilarityMsg (Similarity.ThresholdChanged (String.toInt raw |> Maybe.withDefault (Similarity.currentThreshold model.similarity)))
                        , onToggle = \postId checked -> SimilarityMsg (Similarity.MatchToggled postId checked)
                        , onClearSelection = SimilarityMsg Similarity.SelectionCleared
                        , onCompare = \postId -> SimilarityMsg (Similarity.CompareRequested postId)
                        , onTrash = SimilarityMsg Similarity.TrashRequested
                        , onIndexAnchor = SimilarityMsg Similarity.IndexAnchorRequested
                        , onIndexLibrary = SimilarityMsg Similarity.IndexLibraryRequested
                        , onCancelIndexLibrary = SimilarityMsg Similarity.CancelIndexLibraryRequested
                        , onReviewUncertain = SimilarityMsg Similarity.UncertainReviewed
                        , onMediaError = \postId -> SimilarityMsg (Similarity.MediaFailed postId)
                        }
                    ]

                else
                    []
               )
            ++ (if isAdminEffective model then
                    [ Html.map RecoveryMsg (Recovery.view model.recovery) ]

                else
                    []
               )
        )


navigatorView : Model -> Html Msg
navigatorView model =
    if navigatorVisible model then
        Feature.Navigator.view
            { presentation = navigatorPresentation model
            , current = if Collections.browse model.collections == Nothing then Search.query model.search else ""
            , saved = savedItems model
            , savedStatus = model.savedSearchStatus
            , savedSaving = model.savedSearchSaving
            , localFallback = model.savedSearchFallback
            , recent = model.recent
            , collections = Collections.items model.collections
            , activeCollection = Collections.activeId model.collections
            , collectionBrowse = Collections.browse model.collections
            , collectionStatus = Collections.status model.collections
            , collectionDraft = Collections.draft model.collections
            , onCollectionDraft = CollectionsMsg << Collections.DraftChanged
            , onCreateCollection = CollectionsMsg << Collections.CreateRequested
            , onOpenCollection = OpenCollection
            , onAllPosts = OpenAllPosts
            , summariesUnavailable = Collections.summariesUnavailable model.collections
            , onRetrySummaries = CollectionsMsg Collections.SummariesRetryRequested
            , manage = navigatorManage model
            , onManageCollection = CollectionsMsg << Collections.ManageRequested
            , renamingId = Collections.renamingId model.collections
            , renameDraft = Collections.renameDraft model.collections
            , onRenameDraft = CollectionsMsg << Collections.RenameDraftChanged
            , onStartRename = CollectionsMsg << Collections.RenameRequested
            , onSubmitRename = CollectionsMsg Collections.RenameSubmitted
            , onCancelRename = CollectionsMsg Collections.RenameCancelled
            , deleteConfirmId = Collections.deleteConfirmId model.collections
            , onRequestDelete = CollectionsMsg << Collections.DeleteRequested
            , onConfirmDelete = CollectionsMsg Collections.DeleteConfirmed
            , onCancelDelete = CollectionsMsg Collections.DeleteCancelled
            , manageBusy = Collections.manageBusy model.collections
            , onRemoveCollectionPost = CollectionsMsg << Collections.RemoveRequested
            , removingPosts = Collections.removals model.collections
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
            , showPreview = model.mode == Grid
            , commonTags = commonTags model
            , missing = model.missing
            , onTag = TagClicked
            , onClear = KeyCommand ClearSelection
            , onClose = KeyCommand ToggleInspector
            , collections = Collections.items model.collections
            , activeCollection = Collections.activeId model.collections
            , onSelectCollection = CollectionsMsg << Collections.Selected
            , onFavorite = KeyCommand ToggleFavoriteAction
            , onAddToCollection = AddToCollection
            , onMediaError = MediaFailed
            , writesEnabled = App.Access.canWrite model.access
            , tagAdd = Tags.addDraft model.tags
            , tagRemove = Tags.removeDraft model.tags
            , tagStatus = Tags.status model.tags
            , tagSaving = Tags.saving model.tags
            , onTagAdd = TagsMsg << Tags.AddChanged
            , onTagRemove = TagsMsg << Tags.RemoveChanged
            , onSaveTags = TagsMsg Tags.SaveRequested
            , tagSuggestions = TagSuggestions.suggestions model.tagSuggestions
            , onTagSuggestion = TagSuggestionChosen
            , scoreDraft = Reactions.draft model.reactions
            , reactionSaving = Reactions.saving model.reactions
            , reactionStatus = Reactions.status model.reactions
            , onScoreDraft = ReactionsMsg << Reactions.DraftChanged
            , onStepScore = ReactionsMsg << Reactions.StepScore
            , onSaveScore = ReactionsMsg Reactions.ScoreRequested
            , onRevert = TagsMsg << Tags.RevertRequested
            , onConfirmRevert = TagsMsg << Tags.RevertConfirmed
            , onCancelRevert = TagsMsg Tags.RevertCancelled
            , revertConfirm = Tags.revertConfirm model.tags
            , revertPending = Tags.revertPending model.tags
            , revertStatus = Tags.revertStatus model.tags
            , metadataSource = Metadata.sourceDraft model.metadata
            , metadataArtist = Metadata.artistDraft model.metadata
            , metadataSaving = Metadata.saving model.metadata
            , metadataStatus = Metadata.status model.metadata
            , metadataConflict = Metadata.conflict model.metadata
            , onMetadataSource = MetadataMsg << Metadata.SourceChanged
            , onMetadataArtist = MetadataMsg << Metadata.ArtistChanged
            , onSaveMetadata = MetadataMsg Metadata.SaveRequested
            , onReloadMetadata = MetadataMsg Metadata.ReloadRequested
            , bulkCount = List.length (bulkTargetIds model)
            , bulkOverLimit = BulkMetadata.overLimit model.bulkMetadata
            , bulkLoading = BulkMetadata.loading model.bulkMetadata
            , bulkLoaded = BulkMetadata.loaded model.bulkMetadata
            , bulkCommonSource = BulkMetadata.commonSource model.bulkMetadata
            , bulkCommonArtist = BulkMetadata.commonArtist model.bulkMetadata
            , bulkChangeSource = BulkMetadata.changeSource model.bulkMetadata
            , bulkChangeArtist = BulkMetadata.changeArtist model.bulkMetadata
            , bulkSourceDraft = BulkMetadata.sourceDraft model.bulkMetadata
            , bulkArtistDraft = BulkMetadata.artistDraft model.bulkMetadata
            , bulkSaving = BulkMetadata.saving model.bulkMetadata
            , bulkStatus = BulkMetadata.status model.bulkMetadata
            , bulkConflict = BulkMetadata.conflict model.bulkMetadata
            , bulkNeedsReload = BulkMetadata.needsReload model.bulkMetadata
            , bulkReviewing = BulkMetadata.reviewing model.bulkMetadata
            , onBulkSourceCheck = BulkMetadataMsg << BulkMetadata.SourceCheckChanged
            , onBulkArtistCheck = BulkMetadataMsg << BulkMetadata.ArtistCheckChanged
            , onBulkSource = BulkMetadataMsg << BulkMetadata.SourceChanged
            , onBulkArtist = BulkMetadataMsg << BulkMetadata.ArtistChanged
            , onBulkReview = BulkMetadataMsg BulkMetadata.ReviewRequested
            , onBulkSave = BulkMetadataMsg BulkMetadata.SaveRequested
            , onBulkReload = BulkMetadataMsg BulkMetadata.ReloadRequested
            , ratingTargetCount = List.length (ratingTargetSummaries model)
            , ratingSelected =
                case Ratings.display (ratingTargetSummaries model) model.ratings of
                    Ratings.Uniform rating ->
                        Just rating

                    _ ->
                        Nothing
            , ratingMixed = Ratings.display (ratingTargetSummaries model) model.ratings == Ratings.Mixed
            , ratingUnknownVersion =
                List.length (ratingTargetSummaries model)
                    /= List.length (Selection.targets model.sequence model.selection)
                    || Ratings.hasUnknownVersion (ratingTargets model)
            , ratingDraft = Ratings.draft model.ratings
            , ratingSaving = Ratings.saving model.ratings
            , ratingStatus = Ratings.status model.ratings
            , ratingConflict = Ratings.conflict model.ratings
            , onRating = RatingsMsg << Ratings.DraftChosen
            , onSaveRating = RatingsMsg Ratings.SaveRequested
            , onReloadRating = RatingsMsg Ratings.ReloadRequested
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
            , stale = workspaceBusy model && not (Sequence.isEmpty model.sequence)
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
            (case Collections.browse model.collections of
                Just _ ->
                    case Collections.state model.collections of
                        Collections.Idle ->
                            [ p [] [ text "Enter a search to browse media." ]
                            , button [ class "button", type_ "button", onClick (RunQuery "demo") ] [ text "Try “demo”" ]
                            ]

                        Collections.Loading ->
                            [ p [] [ text "Searching…" ] ]

                        Collections.LoadingMore ->
                            [ p [] [ text "Loading more…" ] ]

                        Collections.Ready ->
                            [ p [] [ text "No posts found." ] ]

                        Collections.Failed message ->
                            [ p [ class "status-error" ] [ text message ]
                            , button [ class "button", type_ "button", onClick Retry ] [ text "Retry" ]
                            ]

                Nothing ->
                    case Search.state model.search of
                        Search.Idle ->
                            [ p [] [ text "Enter a search to browse media." ]
                            , button [ class "button", type_ "button", onClick (RunQuery "demo") ] [ text "Try “demo”" ]
                            ]

                        Search.Searching ->
                            [ p [] [ text "Searching…" ] ]

                        Search.LoadingMore ->
                            [ p [] [ text "Loading more…" ] ]

                        Search.RefreshingPrefix ->
                            [ p [] [ text "Refreshing…" ] ]

                        Search.Ready ->
                            [ p [] [ text "No posts found." ] ]

                        Search.Failed message ->
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
                (if model.mode == Grid then
                    String.fromInt selectedCount ++ " selected · " ++ String.fromInt (Sequence.length model.sequence) ++ " loaded"

                 else
                    String.fromInt ((model.selection.active |> Maybe.andThen (\postId -> Sequence.indexOf postId model.sequence) |> Maybe.withDefault 0) + 1)
                        ++ " of " ++ String.fromInt (Sequence.length model.sequence)
                        ++ " · " ++ String.fromInt selectedCount ++ " selected"
                )
            ]
        , if workspaceBusy model then
            span [ class "status-item", attribute "role" "status" ] [ text "Loading…" ]

          else
            text ""
        , case Collections.browse model.collections of
            Just _ ->
                case Collections.state model.collections of
                    Collections.Failed message ->
                        if Sequence.isEmpty model.sequence then
                            text ""

                        else
                            span [ class "status-item status-error" ]
                                [ text message
                                , button [ class "button button-quiet", type_ "button", onClick Retry ] [ text "Retry" ]
                                ]

                    _ ->
                        text ""

            Nothing ->
                case Search.state model.search of
                    Search.Failed message ->
                        if Sequence.isEmpty model.sequence then
                            text ""

                        else
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
