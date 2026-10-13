module Feature.Collections exposing
    ( Context
    , ManageRow
    , ManageView
    , Model
    , Msg(..)
    , OutMsg(..)
    , Status(..)
    , SummariesState(..)
    , abandonBrowse
    , activeId
    , activeName
    , browse
    , context
    , deleteConfirmId
    , draft
    , errorToString
    , hasMore
    , init
    , isBusy
    , items
    , locked
    , manageBusy
    , manageView
    , removals
    , renameDraft
    , renamingId
    , requestId
    , requestMore
    , requestSummaries
    , state
    , status
    , summariesState
    , summariesUnavailable
    , update
    )

{-| Collection workflows: the summary list and selection, drafts and
notices, pending removals, versioned pagination, the bounded
prefix-refresh lifecycle, bounded ordering management, and
version-checked rename/delete.

Navigation metadata comes only from collection summaries (counts and
versions), never from whole member lists. Ordering management pages
through a bounded window of visible members fetched with the same paged
posts endpoint the grid uses; moves submit a single post plus a real
visible neighbor (or the end) and the observed version, so hidden
members stay server-owned.

The feature owns its own request generation, result cursor, loading
status, and browsed collection. Late search responses can never replace
collection results: search flows clear the browse through
`abandonBrowse`, and every collection completion is dropped unless its
request is current and its collection is still browsed.

Result integration stays with the caller: page arrivals surface as
`OutMsg` values and the page merges posts into its sequence, selection,
mode, and scroll position. Workspace selection, the displayed sequence,
mode, detail cache, access state, navigation, viewport, and focus stay
with the page; per-call page facts arrive through `Context`.
-}

import Api.Collection
import Api.Error as ApiError
import Domain.Collection exposing (CollectionAck, CollectionPostsPage, CollectionSummary, ManageResult, MoveResult)
import Domain.CollectionLogic as CollectionLogic
import Domain.Post exposing (PostSummary)
import Domain.Sequence as Sequence exposing (Sequence)
import Http
import Set exposing (Set)


type Status
    = Idle
    | Loading
    | LoadingMore
    | Ready
    | Failed String


{-| Whether the summary capability answered. Older servers without it
land in `SummariesUnavailable`: the UI offers a retry and never falls
back to whole-member downloads.
-}
type SummariesState
    = SummariesLoading
    | SummariesReady
    | SummariesUnavailable String


{-| A prefix refresh in progress. The previously loaded `target` members
are re-read through bounded first pages while the old sequence stays on
screen; only the fully assembled replacement swaps in. `attempt` counts
automatic restarts after the collection changed mid-refresh.
-}
type alias Refresh =
    { collectionId : String
    , target : Int
    , fetched : Sequence
    , cursor : Maybe String
    , pages : Int
    , attempt : Int
    }


{-| A bounded window of visible members for ordering management. Pages
of at most `collectionPageSize` rows are fetched on demand up to
`maxManagePages`; `page` pages through the fetched window for display.
`version` stays `Nothing` until a page snapshot reports it, so a move
can never invent a version. `pendingMove` resumes one down-move that
needed the next page's neighbor.
-}
type alias ManageState =
    { collectionId : String
    , ids : List String
    , page : Int
    , nextCursor : Maybe String
    , version : Maybe Int
    , loading : Bool
    , pagesFetched : Int
    , chainTarget : Int
    , accumulated : List String
    , pendingMove : Maybe ( String, Int )
    , pendingPage : Maybe Int
    }


type alias Model =
    { items : List CollectionSummary
    , summariesState : SummariesState
    , summariesRequestId : Int
    , activeId : Maybe String
    , browse : Maybe String
    , draft : String
    , status : Maybe String
    , removals : Set String
    , refresh : Maybe Refresh
    , cursor : Maybe String
    , requestId : Int
    , state : Status
    , manage : Maybe ManageState
    , manageRequestId : Int
    , moveInFlight : Maybe String
    , renameFor : Maybe String
    , renameDraft : String
    , deleteConfirmFor : Maybe String
    , manageActionInFlight : Bool
    }


{-| Per-call page facts. Collection reads are public but mutations are
credentialed; `selectedIds` are the add targets and `loadedCount` sizes
the prefix-refresh target. The feature never stores any of it.
-}
type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    , selectedIds : List String
    , loadedCount : Int
    }


context : String -> Maybe String -> Bool -> Int -> List String -> Int -> Context
context apiBase credential writesEnabled accessGeneration selectedIds loadedCount =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    , selectedIds = selectedIds
    , loadedCount = loadedCount
    }


type Msg
    = SummariesCompleted Int (Result Http.Error (List CollectionSummary))
    | SummariesRetryRequested
    | DraftChanged String
    | CreateRequested String
    | Created Int (Result Http.Error CollectionAck)
    | Selected String
    | OpenRequested String
    | PostsCompleted Int String (Result Http.Error CollectionPostsPage)
    | AppendCompleted Int String (Result Http.Error CollectionPostsPage)
    | RefreshCompleted Int String (Result Http.Error CollectionPostsPage)
    | AddRequested
    | Added Int String (Result Http.Error CollectionAck)
    | ManageRequested String
    | ManageCompleted Int String (Result Http.Error CollectionPostsPage)
    | ManageNextRequested
    | ManageBackRequested
    | ManageCloseRequested
    | MoveRequested String Int
    | MoveFirstRequested String
    | MoveLastRequested String
    | MoveCompleted Int String String (Result Http.Error MoveResult)
    | RemoveRequested String
    | RemoveCompleted Int String String (Result Http.Error ())
    | RenameRequested String
    | RenameDraftChanged String
    | RenameSubmitted
    | RenameCancelled
    | Renamed Int String (Result Http.Error ManageResult)
    | DeleteRequested String
    | DeleteCancelled
    | DeleteConfirmed
    | Deleted Int String (Result Http.Error ManageResult)
    | RetryRequested


{-| Page arrivals carry posts for the sequence merge. `BrowseLoaded`
replaces the result set, `AppendLoaded` extends it in place, and
`RefreshLoaded` swaps in the assembled prefix with the remaining
cursor. `BrowseEnded` means the browsed collection was deleted and the
page must clear its grid. `AccessRevoked` asks the page to lock after a
401 on a credentialed mutation.
-}
type OutMsg
    = BrowseLoaded { posts : List PostSummary, nextCursor : Maybe String }
    | AppendLoaded { posts : List PostSummary, nextCursor : Maybe String }
    | RefreshLoaded { sequence : Sequence, nextCursor : Maybe String }
    | BrowseEnded
    | AccessRevoked


init : Model
init =
    { items = []
    , summariesState = SummariesLoading
    , summariesRequestId = 0
    , activeId = Nothing
    , browse = Nothing
    , draft = ""
    , status = Nothing
    , removals = Set.empty
    , refresh = Nothing
    , cursor = Nothing
    , requestId = 0
    , state = Idle
    , manage = Nothing
    , manageRequestId = 0
    , moveInFlight = Nothing
    , renameFor = Nothing
    , renameDraft = ""
    , deleteConfirmFor = Nothing
    , manageActionInFlight = False
    }


items : Model -> List CollectionSummary
items model =
    model.items


activeId : Model -> Maybe String
activeId model =
    model.activeId


activeName : Model -> Maybe String
activeName model =
    model.browse
        |> Maybe.andThen (\collectionId -> List.filter (\c -> c.id == collectionId) model.items |> List.head)
        |> Maybe.map .name


browse : Model -> Maybe String
browse model =
    model.browse


draft : Model -> String
draft model =
    model.draft


status : Model -> Maybe String
status model =
    model.status


removals : Model -> Set String
removals model =
    model.removals


state : Model -> Status
state model =
    model.state


requestId : Model -> Int
requestId model =
    model.requestId


summariesState : Model -> SummariesState
summariesState model =
    model.summariesState


summariesUnavailable : Model -> Maybe String
summariesUnavailable model =
    case model.summariesState of
        SummariesUnavailable message ->
            Just message

        _ ->
            Nothing


renamingId : Model -> Maybe String
renamingId model =
    model.renameFor


renameDraft : Model -> String
renameDraft model =
    model.renameDraft


deleteConfirmId : Model -> Maybe String
deleteConfirmId model =
    model.deleteConfirmFor


manageBusy : Model -> Bool
manageBusy model =
    model.manageActionInFlight || model.moveInFlight /= Nothing


{-| One visible ordering row with its edge capabilities inside the
fetched window. `canDown` also holds at the window end while more
members can still be fetched.
-}
type alias ManageRow =
    { id : String
    , canUp : Bool
    , canDown : Bool
    , canFirst : Bool
    , canLast : Bool
    }


type alias ManageView =
    { collectionId : String
    , name : String
    , rows : List ManageRow
    , page : Int
    , pageCount : Int
    , hasPrev : Bool
    , hasNext : Bool
    , loading : Bool
    , busy : Bool
    }


manageView : Model -> Maybe ManageView
manageView model =
    case ( model.manage, model.activeId ) of
        ( Just manage, Just active ) ->
            if manage.collectionId /= active then
                Nothing

            else
                let
                    summary =
                        List.filter (\c -> c.id == manage.collectionId) model.items |> List.head

                    page =
                        CollectionLogic.clampManagePage (List.length manage.ids) manage.page

                    start =
                        page * CollectionLogic.collectionPageSize

                    moreRows =
                        manage.nextCursor /= Nothing

                    rows =
                        manage.ids
                            |> List.drop start
                            |> List.take CollectionLogic.collectionPageSize
                            |> List.indexedMap
                                (\offset postId ->
                                    let
                                        idx =
                                            start + offset
                                    in
                                    { id = postId
                                    , canUp = idx > 0
                                    , canDown = idx + 1 < List.length manage.ids || moreRows
                                    , canFirst = idx > 0
                                    , canLast = idx + 1 < List.length manage.ids || moreRows
                                    }
                                )
                in
                Just
                    { collectionId = manage.collectionId
                    , name = summary |> Maybe.map .name |> Maybe.withDefault ""
                    , rows = rows
                    , page = page
                    , pageCount = CollectionLogic.managePageCount (List.length manage.ids)
                    , hasPrev = page > 0
                    , hasNext = page + 1 < CollectionLogic.managePageCount (List.length manage.ids) || moreRows
                    , loading = manage.loading
                    , busy = manageBusy model || manage.loading
                    }

        _ ->
            Nothing


{-| A browsed page is ready and another page is available. Scroll
geometry stays with the caller; combine this with the grid mode there.
-}
hasMore : Model -> Bool
hasMore model =
    case ( model.browse, model.state, model.cursor ) of
        ( Just _, Ready, Just _ ) ->
            True

        _ ->
            False


{-| A collection request is in flight. The old results stay on screen
while this is true.
-}
isBusy : Model -> Bool
isBusy model =
    case model.state of
        Loading ->
            True

        LoadingMore ->
            True

        _ ->
            False


{-| Leave collection browsing without touching the collection list,
selection, draft, notices, or removal markers.
-}
abandonBrowse : Model -> Model
abandonBrowse model =
    { model | browse = Nothing, refresh = Nothing, cursor = Nothing, state = Idle }


{-| Credentials went away: notices, in-flight markers, and unconfirmed
rename/delete intent clear. Browsing, refresh work, and the list itself
survive; their completions reconcile through the generation check.
-}
locked : Model -> Model
locked model =
    { model
        | status = Nothing
        , removals = Set.empty
        , moveInFlight = Nothing
        , manageActionInFlight = False
        , renameFor = Nothing
        , renameDraft = ""
        , deleteConfirmFor = Nothing
        , manage = Maybe.map (\manage -> { manage | pendingMove = Nothing }) model.manage
    }


{-| Ask for the next page of the browsed collection. Without a browsed
collection or cursor this is a no-op. Appends reuse the current
generation so a newer browse invalidates them.
-}
requestMore : Context -> Model -> ( Model, Cmd Msg )
requestMore ctx model =
    case ( model.browse, model.cursor ) of
        ( Just collectionId, Just cursor ) ->
            ( { model | state = LoadingMore }
            , Api.Collection.posts ctx.apiBase collectionId (Just cursor) CollectionLogic.collectionPageSize (AppendCompleted model.requestId collectionId)
            )

        _ ->
            ( model, Cmd.none )


{-| Re-read the summary list. Each request bumps its generation so a
late answer can never overwrite a fresher one.
-}
requestSummaries : Context -> Model -> ( Model, Cmd Msg )
requestSummaries ctx model =
    fetchSummaries ctx model


fetchSummaries : Context -> Model -> ( Model, Cmd Msg )
fetchSummaries ctx model =
    let
        id =
            model.summariesRequestId + 1
    in
    ( { model | summariesRequestId = id }
    , Api.Collection.summaries ctx.apiBase (SummariesCompleted id)
    )


summaryOf : String -> Model -> Maybe CollectionSummary
summaryOf collectionId model =
    List.filter (\c -> c.id == collectionId) model.items |> List.head


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        SummariesCompleted id result ->
            if id /= model.summariesRequestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok summaries ->
                        ( { model
                            | items = summaries
                            , summariesState = SummariesReady
                            , activeId =
                                case model.activeId of
                                    Just active ->
                                        if List.any (\collection -> collection.id == active) summaries then
                                            Just active

                                        else
                                            List.head summaries |> Maybe.map .id

                                    Nothing ->
                                        List.head summaries |> Maybe.map .id
                          }
                        , Cmd.none
                        , []
                        )

                    Err error ->
                        ( { model
                            | summariesState = SummariesUnavailable (errorToString error)
                            , status = Just "Collections are unavailable."
                          }
                        , Cmd.none
                        , []
                        )

        SummariesRetryRequested ->
            let
                ( next, cmd ) =
                    fetchSummaries ctx { model | summariesState = SummariesLoading, status = Nothing }
            in
            ( next, cmd, [] )

        DraftChanged value ->
            ( { model | draft = value, status = Nothing }, Cmd.none, [] )

        CreateRequested name ->
            if String.trim name == "" then
                ( model, Cmd.none, [] )

            else if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to create collections." }, Cmd.none, [] )

            else
                ( { model | status = Just "Creating collection…" }
                , Api.Collection.create ctx.apiBase ctx.credential name (Created ctx.accessGeneration)
                , []
                )

        Created generation result ->
            if generation /= ctx.accessGeneration then
                let
                    ( next, cmd ) =
                        fetchSummaries ctx model
                in
                ( next, cmd, [] )

            else
                case result of
                    Ok ack ->
                        let
                            summary =
                                { id = ack.id, name = ack.name, version = ack.version, memberCount = 0, visiblePostCount = 0 }

                            nextItems =
                                if List.any (\c -> c.id == ack.id) model.items then
                                    List.map (\c -> if c.id == ack.id then summary else c) model.items

                                else
                                    model.items ++ [ summary ]
                        in
                        ( { model
                            | items = nextItems
                            , summariesState = SummariesReady
                            , activeId = Just ack.id
                            , draft = ""
                            , status = Just "Collection created."
                          }
                        , Cmd.none
                        , []
                        )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | status = Just (errorToString error) }, Cmd.none, [] )

        Selected id ->
            ( { model
                | activeId = Just id
                , status = Nothing
                , renameFor = Nothing
                , renameDraft = ""
                , deleteConfirmFor = Nothing
              }
            , Cmd.none
            , []
            )

        OpenRequested collectionId ->
            let
                id =
                    model.requestId + 1
            in
            ( { model
                | requestId = id
                , state = Loading
                , cursor = Nothing
                , browse = Just collectionId
                , activeId = Just collectionId
                , refresh = Nothing
                , renameFor = Nothing
                , renameDraft = ""
                , deleteConfirmFor = Nothing
              }
            , Api.Collection.posts ctx.apiBase collectionId Nothing CollectionLogic.collectionPageSize (PostsCompleted id collectionId)
            , []
            )

        PostsCompleted id collectionId result ->
            if CollectionLogic.isObsoleteResponse id model.requestId model.browse collectionId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok response ->
                        ( { model | state = Ready, cursor = response.nextCursor, browse = Just collectionId, refresh = Nothing }
                        , Cmd.none
                        , [ BrowseLoaded { posts = response.posts, nextCursor = response.nextCursor } ]
                        )

                    Err error ->
                        ( { model | state = Failed (errorToString error), browse = Just collectionId }
                        , Cmd.none
                        , []
                        )

        AppendCompleted id collectionId result ->
            if CollectionLogic.isObsoleteResponse id model.requestId model.browse collectionId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok response ->
                        ( { model | state = Ready, cursor = response.nextCursor }
                        , Cmd.none
                        , [ AppendLoaded { posts = response.posts, nextCursor = response.nextCursor } ]
                        )

                    Err error ->
                        appendFailed ctx collectionId error model

        RefreshCompleted id collectionId result ->
            if CollectionLogic.isObsoleteResponse id model.requestId model.browse collectionId then
                ( model, Cmd.none, [] )

            else
                case model.refresh of
                    Nothing ->
                        ( model, Cmd.none, [] )

                    Just refresh ->
                        if refresh.collectionId /= collectionId then
                            ( model, Cmd.none, [] )

                        else
                            case result of
                                Ok response ->
                                    refreshPageArrived ctx collectionId refresh response model

                                Err error ->
                                    refreshPageFailed ctx collectionId refresh error model

        AddRequested ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to add to a collection." }, Cmd.none, [] )

            else
                case model.activeId of
                    Just collectionId ->
                        if List.isEmpty ctx.selectedIds then
                            ( { model | status = Just "Select a post first." }, Cmd.none, [] )

                        else
                            ( { model | status = Just "Adding posts…" }
                            , Api.Collection.addPosts ctx.apiBase ctx.credential collectionId ctx.selectedIds (Added ctx.accessGeneration collectionId)
                            , []
                            )

                    Nothing ->
                        ( { model | status = Just "Create or select a collection first." }, Cmd.none, [] )

        Added generation collectionId result ->
            if generation /= ctx.accessGeneration then
                reconcileStaleMutation ctx collectionId model

            else
                case result of
                    Ok ack ->
                        if ack.id /= collectionId then
                            let
                                ( next, cmd ) =
                                    fetchSummaries ctx { model | status = Just "The add response was invalid." }
                            in
                            ( next, cmd, [] )

                        else
                            let
                                base =
                                    { model | status = Just "Posts added to collection." }

                                ( withSummaries, summariesCmd ) =
                                    fetchSummaries ctx base

                                ( managed, manageCmd ) =
                                    refreshManageIf ctx collectionId withSummaries
                            in
                            if model.browse == Just collectionId then
                                let
                                    ( refreshed, refreshCmd, outs ) =
                                        startPrefixRefresh ctx collectionId
                                            { managed | status = Just "Posts added to collection. Refreshing…" }
                                in
                                ( refreshed, Cmd.batch [ summariesCmd, manageCmd, refreshCmd ], outs )

                            else
                                ( managed, Cmd.batch [ summariesCmd, manageCmd ], [] )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | status = Just (errorToString error) }, Cmd.none, [] )

        ManageRequested collectionId ->
            case summaryOf collectionId model of
                Nothing ->
                    ( model, Cmd.none, [] )

                Just _ ->
                    openManage ctx collectionId { model | activeId = Just collectionId }

        ManageCompleted id collectionId result ->
            managePageArrived ctx id collectionId result model

        ManageNextRequested ->
            case model.manage of
                Just manage ->
                    let
                        pageCount =
                            CollectionLogic.managePageCount (List.length manage.ids)
                    in
                    if manage.page + 1 < pageCount then
                        ( { model | manage = Just { manage | page = manage.page + 1 } }, Cmd.none, [] )

                    else if manage.nextCursor /= Nothing && not manage.loading then
                        fetchMoreManage ctx { manage | pendingPage = Just (manage.page + 1) } model

                    else if manage.nextCursor == Nothing then
                        ( { model | status = Just "Already at the last ordering page." }, Cmd.none, [] )

                    else
                        ( { model | status = Just manageLimitMessage }, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        ManageBackRequested ->
            case model.manage of
                Just manage ->
                    ( { model | manage = Just { manage | page = max 0 (manage.page - 1) } }, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        ManageCloseRequested ->
            ( { model | manage = Nothing, moveInFlight = Nothing }, Cmd.none, [] )

        MoveRequested postId delta ->
            requestMove ctx postId delta model

        MoveFirstRequested postId ->
            case model.manage of
                Just manage ->
                    requestMovePlan ctx postId (CollectionLogic.planFirst manage.ids postId) model

                Nothing ->
                    requestMovePlan ctx postId Nothing model

        MoveLastRequested postId ->
            case model.manage of
                Just manage ->
                    requestMovePlan ctx postId (CollectionLogic.planLast manage.ids (manage.nextCursor /= Nothing) postId) model

                Nothing ->
                    requestMovePlan ctx postId Nothing model

        MoveCompleted generation collectionId postId result ->
            moveCompleted ctx generation collectionId postId result model

        RemoveRequested postId ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to remove from collections." }, Cmd.none, [] )

            else
                case model.activeId of
                    Nothing ->
                        ( { model | status = Just "Select a collection first." }, Cmd.none, [] )

                    Just collectionId ->
                        if Set.member postId model.removals then
                            ( model, Cmd.none, [] )

                        else if not (manageAllowsRemoval postId collectionId model) then
                            ( model, Cmd.none, [] )

                        else
                            ( { model | status = Just "Removing…", removals = Set.insert postId model.removals }
                            , Api.Collection.removePost ctx.apiBase ctx.credential collectionId postId (RemoveCompleted ctx.accessGeneration collectionId postId)
                            , []
                            )

        RemoveCompleted generation collectionId postId result ->
            if generation /= ctx.accessGeneration then
                reconcileStaleRemoval ctx collectionId postId model

            else
                case result of
                    Ok () ->
                        let
                            base =
                                { model | removals = Set.remove postId model.removals, status = Just "Removed from collection." }

                            ( withSummaries, summariesCmd ) =
                                fetchSummaries ctx base

                            ( managed, manageCmd ) =
                                refreshManageIf ctx collectionId withSummaries
                        in
                        if model.browse == Just collectionId then
                            let
                                ( refreshed, refreshCmd, outs ) =
                                    startPrefixRefresh ctx collectionId
                                        { managed | status = Just "Removed from collection. Refreshing…" }
                            in
                            ( refreshed, Cmd.batch [ summariesCmd, manageCmd, refreshCmd ], outs )

                        else
                            ( managed, Cmd.batch [ summariesCmd, manageCmd ], [] )

                    Err (Http.BadStatus 401) ->
                        ( { model | removals = Set.remove postId model.removals }, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | removals = Set.remove postId model.removals, status = Just (errorToString error) }, Cmd.none, [] )

        RenameRequested collectionId ->
            case summaryOf collectionId model of
                Nothing ->
                    ( model, Cmd.none, [] )

                Just summary ->
                    ( { model
                        | renameFor = Just collectionId
                        , renameDraft = summary.name
                        , deleteConfirmFor = Nothing
                        , status = Nothing
                      }
                    , Cmd.none
                    , []
                    )

        RenameDraftChanged value ->
            ( { model | renameDraft = value, status = Nothing }, Cmd.none, [] )

        RenameCancelled ->
            ( { model | renameFor = Nothing, renameDraft = "", status = Nothing }, Cmd.none, [] )

        RenameSubmitted ->
            case model.renameFor of
                Nothing ->
                    ( model, Cmd.none, [] )

                Just collectionId ->
                    if not ctx.writesEnabled then
                        ( { model | status = Just "Unlock to rename collections." }, Cmd.none, [] )

                    else if model.manageActionInFlight then
                        ( model, Cmd.none, [] )

                    else
                        case summaryOf collectionId model of
                            Nothing ->
                                ( { model
                                    | renameFor = Nothing
                                    , renameDraft = ""
                                    , status = Just "That collection is no longer available."
                                  }
                                , Cmd.none
                                , []
                                )

                            Just summary ->
                                let
                                    trimmed =
                                        String.trim model.renameDraft
                                in
                                if trimmed == "" then
                                    ( { model | status = Just "Enter a name." }, Cmd.none, [] )

                                else if trimmed == summary.name then
                                    ( { model
                                        | renameFor = Nothing
                                        , renameDraft = ""
                                        , status = Just "Name unchanged."
                                      }
                                    , Cmd.none
                                    , []
                                    )

                                else
                                    ( { model | status = Just "Renaming…", manageActionInFlight = True }
                                    , Api.Collection.rename ctx.apiBase ctx.credential collectionId trimmed summary.version (Renamed ctx.accessGeneration collectionId)
                                    , []
                                    )

        Renamed generation collectionId result ->
            if generation /= ctx.accessGeneration then
                let
                    ( next, cmd, outs ) =
                        reconcileStaleMutation ctx collectionId { model | manageActionInFlight = False }
                in
                ( next, cmd, outs )

            else
                case result of
                    Ok manageResult ->
                        if manageResult.id /= collectionId then
                            let
                                ( next, cmd ) =
                                    fetchSummaries ctx
                                        { model
                                            | manageActionInFlight = False
                                            , renameFor = Nothing
                                            , renameDraft = ""
                                            , status = Just "The rename response was invalid."
                                        }
                            in
                            ( next, cmd, [] )

                        else
                            ( { model
                                | items =
                                    List.map
                                        (\c ->
                                            if c.id == collectionId then
                                                { c | name = manageResult.name, version = manageResult.version }

                                            else
                                                c
                                        )
                                        model.items
                                , manageActionInFlight = False
                                , renameFor = Nothing
                                , renameDraft = ""
                                , status =
                                    Just
                                        (if manageResult.changed then
                                            "Collection renamed."

                                         else
                                            "Name unchanged."
                                        )
                              }
                            , Cmd.none
                            , []
                            )

                    Err (Http.BadStatus 401) ->
                        ( { model | manageActionInFlight = False }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 409) ->
                        conflictRecovery ctx collectionId "The collection changed. Refreshing…"
                            { model | manageActionInFlight = False }

                    Err error ->
                        ( { model | manageActionInFlight = False, status = Just (errorToString error) }, Cmd.none, [] )

        DeleteRequested collectionId ->
            case summaryOf collectionId model of
                Nothing ->
                    ( model, Cmd.none, [] )

                Just _ ->
                    ( { model
                        | deleteConfirmFor = Just collectionId
                        , renameFor = Nothing
                        , renameDraft = ""
                        , status = Nothing
                      }
                    , Cmd.none
                    , []
                    )

        DeleteCancelled ->
            ( { model | deleteConfirmFor = Nothing, status = Nothing }, Cmd.none, [] )

        DeleteConfirmed ->
            case model.deleteConfirmFor of
                Nothing ->
                    ( model, Cmd.none, [] )

                Just collectionId ->
                    if not ctx.writesEnabled then
                        ( { model | status = Just "Unlock to delete collections." }, Cmd.none, [] )

                    else if model.manageActionInFlight then
                        ( model, Cmd.none, [] )

                    else
                        case summaryOf collectionId model of
                            Nothing ->
                                ( { model
                                    | deleteConfirmFor = Nothing
                                    , status = Just "That collection is no longer available."
                                  }
                                , Cmd.none
                                , []
                                )

                            Just summary ->
                                ( { model | status = Just "Deleting collection…", manageActionInFlight = True }
                                , Api.Collection.deleteCollection ctx.apiBase ctx.credential collectionId summary.version (Deleted ctx.accessGeneration collectionId)
                                , []
                                )

        Deleted generation collectionId result ->
            if generation /= ctx.accessGeneration then
                reconcileStaleDelete ctx collectionId result
                    { model | manageActionInFlight = False, deleteConfirmFor = Nothing }

            else
                case result of
                    Ok manageResult ->
                        if manageResult.id /= collectionId then
                            let
                                ( next, cmd ) =
                                    fetchSummaries ctx
                                        { model
                                            | manageActionInFlight = False
                                            , deleteConfirmFor = Nothing
                                            , status = Just "The delete response was invalid."
                                        }
                            in
                            ( next, cmd, [] )

                        else
                            let
                                ( base, outs ) =
                                    applyDeletedCollection collectionId model
                            in
                            ( base, Cmd.none, outs )

                    Err (Http.BadStatus 401) ->
                        ( { model | manageActionInFlight = False }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 409) ->
                        conflictRecovery ctx collectionId "The collection changed. Refreshing…"
                            { model | manageActionInFlight = False }

                    Err error ->
                        ( { model | manageActionInFlight = False, status = Just (errorToString error) }, Cmd.none, [] )

        RetryRequested ->
            case model.browse of
                Just collectionId ->
                    -- Retry rebuilds the browsed prefix without resetting
                    -- the workspace; a plain re-open would jump to page one.
                    startPrefixRefresh ctx collectionId { model | status = Nothing }

                Nothing ->
                    ( model, Cmd.none, [] )


manageAllowsRemoval : String -> String -> Model -> Bool
manageAllowsRemoval postId collectionId model =
    case model.manage of
        Just manage ->
            if manage.collectionId == collectionId then
                List.member postId manage.ids

            else
                True

        Nothing ->
            True


openManage : Context -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
openManage ctx collectionId model =
    let
        id =
            model.manageRequestId + 1

        manage =
            { collectionId = collectionId
            , ids = []
            , page = 0
            , nextCursor = Nothing
            , version = Nothing
            , loading = True
            , pagesFetched = 0
            , chainTarget = CollectionLogic.collectionPageSize
            , accumulated = []
            , pendingMove = Nothing
            , pendingPage = Nothing
            }
    in
    ( { model | manage = Just manage, manageRequestId = id, status = Nothing }
    , Api.Collection.posts ctx.apiBase collectionId Nothing CollectionLogic.collectionPageSize (ManageCompleted id collectionId)
    , []
    )


manageLimitMessage : String
manageLimitMessage =
    "Ordering limit reached: only the first "
        ++ String.fromInt CollectionLogic.maxManagePages
        ++ " pages ("
        ++ String.fromInt (CollectionLogic.maxManagePages * CollectionLogic.collectionPageSize)
        ++ " rows) can be ordered here. Use single-post CLI moves beyond this window."


fetchMoreManage : Context -> ManageState -> Model -> ( Model, Cmd Msg, List OutMsg )
fetchMoreManage ctx manage model =
    case manage.nextCursor of
        Nothing ->
            ( model, Cmd.none, [] )

        Just cursor ->
            if manage.pagesFetched >= CollectionLogic.maxManagePages then
                ( { model | status = Just manageLimitMessage }, Cmd.none, [] )

            else
                let
                    id =
                        model.manageRequestId + 1

                    next =
                        { manage
                            | loading = True
                            , accumulated = manage.ids
                            , chainTarget = List.length manage.ids + CollectionLogic.collectionPageSize
                        }
                in
                ( { model | manage = Just next, manageRequestId = id }
                , Api.Collection.posts ctx.apiBase manage.collectionId (Just cursor) CollectionLogic.collectionPageSize (ManageCompleted id manage.collectionId)
                , []
                )


{-| Re-read a managed window after a mutation, bounded to the pages it
already spanned. Returns the model unchanged when another (or no)
collection is managed, so unrelated workspaces never churn.
-}
refreshManageIf : Context -> String -> Model -> ( Model, Cmd Msg )
refreshManageIf ctx collectionId model =
    case model.manage of
        Just manage ->
            if manage.collectionId /= collectionId then
                ( model, Cmd.none )

            else
                let
                    id =
                        model.manageRequestId + 1

                    next =
                        { manage
                            | loading = True
                            , accumulated = []
                            , pagesFetched = 0
                            , chainTarget = max CollectionLogic.collectionPageSize (List.length manage.ids)
                            , pendingMove = Nothing
                            , pendingPage = Nothing
                        }
                in
                ( { model | manage = Just next, manageRequestId = id }
                , Api.Collection.posts ctx.apiBase collectionId Nothing CollectionLogic.collectionPageSize (ManageCompleted id collectionId)
                )

        Nothing ->
            ( model, Cmd.none )


managePageArrived : Context -> Int -> String -> Result Http.Error CollectionPostsPage -> Model -> ( Model, Cmd Msg, List OutMsg )
managePageArrived ctx id collectionId result model =
    if id /= model.manageRequestId then
        ( model, Cmd.none, [] )

    else
        case model.manage of
            Nothing ->
                ( model, Cmd.none, [] )

            Just manage ->
                if manage.collectionId /= collectionId || not manage.loading then
                    ( model, Cmd.none, [] )

                else
                    case result of
                        Err error ->
                            case classifyCollectionError error of
                                CollectionLogic.CollectionChanged ->
                                    ( { model
                                        | manage = Just { manage | loading = False, pendingMove = Nothing }
                                        , status = Just "The collection changed. Reopen ordering to retry."
                                      }
                                    , Cmd.none
                                    , []
                                    )

                                _ ->
                                    ( { model
                                        | manage = Just { manage | loading = False, pendingMove = Nothing }
                                        , status = Just (errorToString error)
                                      }
                                    , Cmd.none
                                    , []
                                    )

                        Ok response ->
                            let
                                newIds =
                                    manage.accumulated
                                        ++ (List.map .id response.posts
                                                |> List.filter (\postId -> not (List.member postId manage.accumulated))
                                           )

                                pagesFetched =
                                    manage.pagesFetched + 1

                                chained =
                                    { manage
                                        | accumulated = newIds
                                        , pagesFetched = pagesFetched
                                        , version = Just response.collectionVersion
                                    }
                            in
                            if CollectionLogic.needsMoreRefreshPages (List.length newIds) manage.chainTarget response.nextCursor && pagesFetched < CollectionLogic.maxManagePages then
                                let
                                    nextId =
                                        model.manageRequestId + 1
                                in
                                ( { model | manage = Just chained, manageRequestId = nextId }
                                , Api.Collection.posts ctx.apiBase collectionId response.nextCursor CollectionLogic.collectionPageSize (ManageCompleted nextId collectionId)
                                , []
                                )

                            else
                                let
                                    settled =
                                        { chained
                                            | ids = newIds
                                            , nextCursor = response.nextCursor
                                            , loading = False
                                            , accumulated = []
                                            , pendingPage = Nothing
                                            , page =
                                                CollectionLogic.clampManagePage (List.length newIds)
                                                    (manage.pendingPage |> Maybe.withDefault manage.page)
                                        }

                                    base =
                                        { model | manage = Just settled }
                                in
                                case manage.pendingMove of
                                    Just ( postId, delta ) ->
                                        requestMove ctx postId delta
                                            { base | manage = Just { settled | pendingMove = Nothing } }

                                    Nothing ->
                                        ( base, Cmd.none, [] )


requestMove : Context -> String -> Int -> Model -> ( Model, Cmd Msg, List OutMsg )
requestMove ctx postId delta model =
    case model.manage of
        Just manage ->
            requestMovePlan ctx postId
                (CollectionLogic.planMove manage.ids (manage.nextCursor /= Nothing) postId delta)
                model
                |> resumePendingMove ctx postId delta manage

        Nothing ->
            requestMovePlan ctx postId Nothing model


{-| When a one-step move has no plan only because the neighbor sits on
an unfetched page, fetch exactly one more bounded page and resume the
move once; at the fetch bound the move stays a no-op.
-}
resumePendingMove : Context -> String -> Int -> ManageState -> ( Model, Cmd Msg, List OutMsg ) -> ( Model, Cmd Msg, List OutMsg )
resumePendingMove ctx postId delta manage ( settled, cmd, outs ) =
    case ( settled.manage, settled.moveInFlight ) of
        ( Just current, Nothing ) ->
            if delta == 1
                && current.collectionId == manage.collectionId
                && current.nextCursor /= Nothing
                && not current.loading
                && List.member postId current.ids
                && current.pagesFetched < CollectionLogic.maxManagePages
            then
                let
                    ( fetching, fetchCmd, fetchOuts ) =
                        fetchMoreManage ctx { current | pendingMove = Just ( postId, delta ) } settled
                in
                ( { fetching | status = Just "Loading more ordering rows…" }, fetchCmd, fetchOuts )

            else
                ( settled, cmd, outs )

        _ ->
            ( settled, cmd, outs )


requestMovePlan : Context -> String -> Maybe (Maybe String) -> Model -> ( Model, Cmd Msg, List OutMsg )
requestMovePlan ctx postId plan model =
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to reorder collections." }, Cmd.none, [] )

    else if model.moveInFlight /= Nothing then
        ( model, Cmd.none, [] )

    else
        case model.activeId of
            Nothing ->
                ( { model | status = Just "Select a collection first." }, Cmd.none, [] )

            Just collectionId ->
                case model.manage of
                    Nothing ->
                        ( { model | status = Just "Open ordering to move posts." }, Cmd.none, [] )

                    Just manage ->
                        if manage.collectionId /= collectionId then
                            ( { model | status = Just "Open ordering to move posts." }, Cmd.none, [] )

                        else if manage.loading then
                            ( model, Cmd.none, [] )

                        else
                            case manage.version of
                                Nothing ->
                                    ( { model | status = Just "Ordering is still loading…" }, Cmd.none, [] )

                                Just version ->
                                    case plan of
                                        Nothing ->
                                            ( { model
                                                | status =
                                                    Just
                                                        (if manage.nextCursor /= Nothing && manage.pagesFetched >= CollectionLogic.maxManagePages then
                                                            manageLimitMessage

                                                         else
                                                            "Already at that edge — no move needed."
                                                        )
                                              }
                                            , Cmd.none
                                            , []
                                            )

                                        Just beforePostId ->
                                            ( { model | status = Just "Reordering…", moveInFlight = Just postId }
                                            , Api.Collection.move ctx.apiBase ctx.credential collectionId postId beforePostId version (MoveCompleted ctx.accessGeneration collectionId postId)
                                            , []
                                            )


moveCompleted : Context -> Int -> String -> String -> Result Http.Error MoveResult -> Model -> ( Model, Cmd Msg, List OutMsg )
moveCompleted ctx generation collectionId postId result model =
    if generation /= ctx.accessGeneration then
        reconcileStaleMutation ctx collectionId { model | moveInFlight = Nothing }

    else
        case result of
            Ok moveResult ->
                if moveResult.collectionId /= collectionId || moveResult.id /= collectionId then
                    let
                        ( withSummaries, summariesCmd ) =
                            fetchSummaries ctx { model | moveInFlight = Nothing, status = Just "The move response was invalid." }

                        ( managed, manageCmd ) =
                            refreshManageIf ctx collectionId withSummaries
                    in
                    ( managed, Cmd.batch [ summariesCmd, manageCmd ], [] )

                else
                    let
                        base =
                            { model
                                | moveInFlight = Nothing
                                , items =
                                    List.map
                                        (\c ->
                                            if c.id == collectionId then
                                                { c | version = moveResult.version }

                                            else
                                                c
                                        )
                                        model.items
                                , status = Just "Order saved."
                                , manage =
                                    Maybe.map
                                        (\manage ->
                                            if manage.collectionId == collectionId then
                                                { manage | version = Just moveResult.version }

                                            else
                                                manage
                                        )
                                        model.manage
                            }

                        ( managed, manageCmd ) =
                            refreshManageIf ctx collectionId base
                    in
                    if model.browse == Just collectionId then
                        let
                            ( refreshed, refreshCmd, outs ) =
                                startPrefixRefresh ctx collectionId
                                    { managed | status = Just "Order saved. Refreshing…" }
                        in
                        ( refreshed, Cmd.batch [ manageCmd, refreshCmd ], outs )

                    else
                        ( managed, manageCmd, [] )

            Err (Http.BadStatus 401) ->
                ( { model | moveInFlight = Nothing }, Cmd.none, [ AccessRevoked ] )

            Err (Http.BadStatus 409) ->
                conflictRecovery ctx collectionId "The collection changed. Refreshing…"
                    { model | moveInFlight = Nothing }

            Err error ->
                ( { model | moveInFlight = Nothing, status = Just (errorToString error) }, Cmd.none, [] )


{-| A version conflict (or another refresh-worthy failure) reconciles
only the affected collection: summaries re-read, the managed window
rebuilds when open, and the workspace prefix refreshes only while that
collection is still browsed.
-}
conflictRecovery : Context -> String -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
conflictRecovery ctx collectionId notice model =
    let
        ( withSummaries, summariesCmd ) =
            fetchSummaries ctx { model | status = Just notice }

        ( managed, manageCmd ) =
            refreshManageIf ctx collectionId withSummaries
    in
    if model.browse == Just collectionId then
        let
            ( refreshed, refreshCmd, outs ) =
                startPrefixRefresh ctx collectionId managed
        in
        ( refreshed, Cmd.batch [ summariesCmd, manageCmd, refreshCmd ], outs )

    else
        ( managed, Cmd.batch [ summariesCmd, manageCmd ], [] )


{-| Re-read the previously loaded prefix through bounded first pages.
Bumping the generation invalidates outstanding appends; `LoadingMore`
keeps the old results on screen and blocks further appends until the
assembled replacement is ready. A caller-set status is preserved so
mutation confirmations stay visible during the refresh.
-}
startPrefixRefresh : Context -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
startPrefixRefresh ctx collectionId model =
    let
        id =
            model.requestId + 1

        notice =
            case model.status of
                Just current ->
                    Just current

                Nothing ->
                    Just "Refreshing collection…"
    in
    ( { model
        | requestId = id
        , state = LoadingMore
        , browse = Just collectionId
        , refresh =
            Just
                { collectionId = collectionId
                , target = CollectionLogic.refreshTarget ctx.loadedCount
                , fetched = Sequence.empty
                , cursor = Nothing
                , pages = 0
                , attempt = 0
                }
        , status = notice
      }
    , Api.Collection.posts ctx.apiBase collectionId Nothing CollectionLogic.collectionPageSize (RefreshCompleted id collectionId)
    , []
    )


{-| Restart a refresh whose page hit a changed collection or an invalid
cursor. Restarts are bounded: past the attempt limit the refresh stops
with a manual Retry instead of looping.
-}
restartPrefixRefresh : Context -> String -> Refresh -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
restartPrefixRefresh ctx collectionId refresh notice model =
    if CollectionLogic.shouldRetryRefresh refresh.attempt then
        let
            id =
                model.requestId + 1
        in
        ( { model
            | requestId = id
            , state = LoadingMore
            , browse = Just collectionId
            , refresh =
                Just { refresh | fetched = Sequence.empty, cursor = Nothing, pages = 0, attempt = refresh.attempt + 1 }
            , status = Just notice
          }
        , Api.Collection.posts ctx.apiBase collectionId Nothing CollectionLogic.collectionPageSize (RefreshCompleted id collectionId)
        , []
        )

    else
        ( { model
            | state = Failed "The collection keeps changing. Press Retry to reload."
            , refresh = Nothing
            , status = Just "The collection keeps changing. Press Retry to reload."
          }
        , Cmd.none
        , []
        )


{-| Durable deletion applied locally: metadata removed, managed
window for it closed, and — only when it is still browsed — the
browse clears with `BrowseEnded` so the page drops its grid.
-}
applyDeletedCollection : String -> Model -> ( Model, List OutMsg )
applyDeletedCollection collectionId model =
    let
        remaining =
            List.filter (\c -> c.id /= collectionId) model.items

        wasBrowsed =
            model.browse == Just collectionId

        base =
            { model
                | items = remaining
                , summariesState = SummariesReady
                , manageActionInFlight = False
                , deleteConfirmFor = Nothing
                , activeId =
                    if model.activeId == Just collectionId then
                        List.head remaining |> Maybe.map .id

                    else
                        model.activeId
                , manage =
                    case model.manage of
                        Just manage ->
                            if manage.collectionId == collectionId then
                                Nothing

                            else
                                model.manage

                        Nothing ->
                            Nothing
                , status = Just "Collection deleted. Its posts were not deleted."
            }
    in
    if wasBrowsed then
        ( { base | browse = Nothing, refresh = Nothing, cursor = Nothing, state = Idle, requestId = model.requestId + 1 }
        , [ BrowseEnded ]
        )

    else
        ( base, [] )


{-| A delete settled after its access epoch ended. A success is
durable: reconcile that specific deletion instead of refreshing a
now-404 collection and leaving a ghost grid. A stale 401 belongs to
the old epoch and never revokes newer access; other stale failures
only re-read public metadata.
-}
reconcileStaleDelete : Context -> String -> Result Http.Error ManageResult -> Model -> ( Model, Cmd Msg, List OutMsg )
reconcileStaleDelete ctx collectionId result model =
    case result of
        Ok manageResult ->
            if manageResult.id == collectionId then
                let
                    ( base, outs ) =
                        applyDeletedCollection collectionId model
                in
                ( base, Cmd.none, outs )

            else
                let
                    ( next, cmd ) =
                        fetchSummaries ctx model
                in
                ( next, cmd, [] )

        Err (Http.BadStatus 401) ->
            ( model, Cmd.none, [] )

        Err _ ->
            let
                ( next, cmd ) =
                    fetchSummaries ctx model
            in
            ( next, cmd, [] )


{-| A collection mutation settled after its access epoch ended (lock,
unlock, or rediscovery moved the generation). The server may still
have applied it, so public state reconciles — but the mutation itself
is never replayed, credentials and lock state are untouched, and a 401
from the old epoch never revokes current access.
-}
reconcileStaleMutation : Context -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
reconcileStaleMutation ctx collectionId model =
    case CollectionLogic.staleMutationRecovery model.browse collectionId of
        CollectionLogic.RefreshBrowsedCollection ->
            let
                ( refreshed, refreshCmd, outs ) =
                    startPrefixRefresh ctx collectionId { model | status = Nothing }

                ( withSummaries, summariesCmd ) =
                    fetchSummaries ctx refreshed
            in
            ( withSummaries, Cmd.batch [ refreshCmd, summariesCmd ], outs )

        CollectionLogic.RefreshMetadataOnly ->
            let
                ( next, cmd ) =
                    fetchSummaries ctx model
            in
            ( next, cmd, [] )


{-| Stale removal completions reconcile like other mutations, and the
in-flight removal marker clears because its request has settled.
-}
reconcileStaleRemoval : Context -> String -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
reconcileStaleRemoval ctx collectionId postId model =
    reconcileStaleMutation ctx collectionId
        { model | removals = Set.remove postId model.removals }


{-| One refresh page failed. Changed collections and invalid cursors
restart the bounded refresh; anything else keeps the old results with
a manual Retry.
-}
refreshPageFailed : Context -> String -> Refresh -> Http.Error -> Model -> ( Model, Cmd Msg, List OutMsg )
refreshPageFailed ctx collectionId refresh error model =
    case classifyCollectionError error of
        CollectionLogic.CollectionChanged ->
            restartPrefixRefresh ctx collectionId refresh "The collection changed again. Retrying…" model

        CollectionLogic.InvalidCursor ->
            restartPrefixRefresh ctx collectionId refresh "The page cursor was invalid. Reloading…" model

        CollectionLogic.TransportError ->
            ( { model
                | state = Failed (errorToString error)
                , refresh = Nothing
                , status = Just (errorToString error)
              }
            , Cmd.none
            , []
            )


{-| One collection append failed. Changed collections and invalid
cursors restart through the bounded prefix refresh; anything else keeps
the old results with a manual Retry.
-}
appendFailed : Context -> String -> Http.Error -> Model -> ( Model, Cmd Msg, List OutMsg )
appendFailed ctx collectionId error model =
    case classifyCollectionError error of
        CollectionLogic.CollectionChanged ->
            startPrefixRefresh ctx collectionId
                { model | status = Just "The collection changed. Refreshing…" }

        CollectionLogic.InvalidCursor ->
            startPrefixRefresh ctx collectionId
                { model | status = Just "The page cursor was invalid. Reloading…" }

        CollectionLogic.TransportError ->
            ( { model
                | state = Failed (errorToString error)
                , status = Just (errorToString error)
              }
            , Cmd.none
            , []
            )


{-| One prefix-refresh page arrived. Either chain the next bounded page
or swap in the assembled replacement; the old results stay visible
until that swap. The page reconciles selection and scroll against the
completed window only.
-}
refreshPageArrived : Context -> String -> Refresh -> CollectionPostsPage -> Model -> ( Model, Cmd Msg, List OutMsg )
refreshPageArrived ctx collectionId refresh response model =
    let
        fetched =
            Sequence.append response.posts refresh.fetched

        pages =
            refresh.pages + 1

        next =
            { refresh | fetched = fetched, cursor = response.nextCursor, pages = pages }
    in
    if CollectionLogic.needsMoreRefreshPages (Sequence.length fetched) refresh.target response.nextCursor && pages < CollectionLogic.maxRefreshPages then
        let
            id =
                model.requestId + 1
        in
        ( { model | requestId = id, refresh = Just next }
        , Api.Collection.posts ctx.apiBase collectionId response.nextCursor CollectionLogic.collectionPageSize (RefreshCompleted id collectionId)
        , []
        )

    else
        let
            settled =
                { model
                    | state = Ready
                    , cursor = next.cursor
                    , browse = Just collectionId
                    , refresh = Nothing
                    , status = Nothing
                }

            ( withSummaries, summariesCmd ) =
                fetchSummaries ctx settled
        in
        ( withSummaries
        , summariesCmd
        , [ RefreshLoaded { sequence = next.fetched, nextCursor = next.cursor } ]
        )


{-| Transport failures reuse the workspace wording verbatim; the shared
adapter classifies HTTP facts while each feature keeps its own copy.
-}
errorToString : Http.Error -> String
errorToString error =
    case ApiError.classify error of
        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The search timed out."

        ApiError.NetworkError ->
            "The search server could not be reached."

        ApiError.BadStatus statusCode ->
            "Search failed (HTTP " ++ String.fromInt statusCode ++ ")."

        ApiError.BadBody ->
            "The search response was invalid."


classifyCollectionError : Http.Error -> CollectionLogic.CollectionFailure
classifyCollectionError error =
    case ApiError.classify error of
        ApiError.BadStatus statusCode ->
            CollectionLogic.classifyCollectionStatus statusCode

        _ ->
            CollectionLogic.TransportError
