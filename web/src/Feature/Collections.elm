module Feature.Collections exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , Status(..)
    , abandonBrowse
    , activeId
    , activeName
    , browse
    , context
    , draft
    , errorToString
    , hasMore
    , init
    , isBusy
    , items
    , locked
    , removals
    , requestId
    , requestMore
    , state
    , status
    , update
    )

{-| Collection workflows: the collection list and selection, drafts and
notices, pending removals, versioned pagination, and the bounded
prefix-refresh lifecycle.

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
import Domain.Collection exposing (Collection, CollectionPostsPage)
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


type alias Model =
    { items : List Collection
    , activeId : Maybe String
    , browse : Maybe String
    , draft : String
    , status : Maybe String
    , removals : Set String
    , refresh : Maybe Refresh
    , cursor : Maybe String
    , requestId : Int
    , state : Status
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
    = ListCompleted (Result Http.Error (List Collection))
    | DraftChanged String
    | CreateRequested String
    | Created Int (Result Http.Error Collection)
    | Selected String
    | OpenRequested String
    | PostsCompleted Int String (Result Http.Error CollectionPostsPage)
    | AppendCompleted Int String (Result Http.Error CollectionPostsPage)
    | RefreshCompleted Int String (Result Http.Error CollectionPostsPage)
    | AddRequested
    | Added Int String (Result Http.Error Collection)
    | MoveRequested String Int
    | Reordered Int String (Result Http.Error Collection)
    | RemoveRequested String
    | RemoveCompleted Int String String (Result Http.Error ())
    | RetryRequested


{-| Page arrivals carry posts for the sequence merge. `BrowseLoaded`
replaces the result set, `AppendLoaded` extends it in place, and
`RefreshLoaded` swaps in the assembled prefix with the remaining
cursor. `AccessRevoked` asks the page to lock after a 401 on a
credentialed mutation.
-}
type OutMsg
    = BrowseLoaded { posts : List PostSummary, nextCursor : Maybe String }
    | AppendLoaded { posts : List PostSummary, nextCursor : Maybe String }
    | RefreshLoaded { sequence : Sequence, nextCursor : Maybe String }
    | AccessRevoked


init : Model
init =
    { items = []
    , activeId = Nothing
    , browse = Nothing
    , draft = ""
    , status = Nothing
    , removals = Set.empty
    , refresh = Nothing
    , cursor = Nothing
    , requestId = 0
    , state = Idle
    }


items : Model -> List Collection
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


{-| Credentials went away: notices and in-flight removal markers clear.
Browsing, refresh work, and the list itself survive; their completions
reconcile through the generation check.
-}
locked : Model -> Model
locked model =
    { model | status = Nothing, removals = Set.empty }


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


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        ListCompleted result ->
            case result of
                Ok collections ->
                    ( { model
                        | items = collections
                        , activeId =
                            case model.activeId of
                                Just id ->
                                    if List.any (\collection -> collection.id == id) collections then
                                        Just id

                                    else
                                        List.head collections |> Maybe.map .id

                                Nothing ->
                                    List.head collections |> Maybe.map .id
                      }
                    , Cmd.none
                    , []
                    )

                Err error ->
                    ( { model | status = Just (errorToString error) }, Cmd.none, [] )

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
                ( model, Api.Collection.list ctx.apiBase ListCompleted, [] )

            else
                case result of
                    Ok collection ->
                        ( { model
                            | items = model.items ++ [ collection ]
                            , activeId = Just collection.id
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
            ( { model | activeId = Just id, status = Nothing }, Cmd.none, [] )

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
                    Ok updated ->
                        let
                            nextItems =
                                List.map (\collection -> if collection.id == updated.id then updated else collection) model.items

                            base =
                                { model | items = nextItems, status = Just "Posts added to collection." }
                        in
                        if model.browse == Just collectionId then
                            startPrefixRefresh ctx collectionId
                                { base | status = Just "Posts added to collection. Refreshing…" }

                        else
                            ( base, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | status = Just (errorToString error) }, Cmd.none, [] )

        MoveRequested postId delta ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to reorder collections." }, Cmd.none, [] )

            else
                case model.activeId of
                    Nothing ->
                        ( { model | status = Just "Select a collection first." }, Cmd.none, [] )

                    Just collectionId ->
                        case List.filter (\c -> c.id == collectionId) model.items |> List.head of
                            Nothing ->
                                ( { model | status = Just "Select a collection first." }, Cmd.none, [] )

                            Just collection ->
                                case moveInList postId delta collection.postIds of
                                    Nothing ->
                                        ( model, Cmd.none, [] )

                                    Just reordered ->
                                        ( { model | status = Just "Reordering…" }
                                        , Api.Collection.reorder ctx.apiBase ctx.credential collectionId reordered (Reordered ctx.accessGeneration collectionId)
                                        , []
                                        )

        Reordered generation collectionId result ->
            if generation /= ctx.accessGeneration then
                reconcileStaleMutation ctx collectionId model

            else
                case result of
                    Ok updated ->
                        let
                            nextItems =
                                List.map (\c -> if c.id == updated.id then updated else c) model.items

                            base =
                                { model | items = nextItems, status = Just "Order saved." }
                        in
                        if model.browse == Just collectionId then
                            startPrefixRefresh ctx collectionId
                                { base | status = Just "Order saved. Refreshing…" }

                        else
                            ( base, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | status = Just (errorToString error) }, Cmd.none, [] )

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

                        else
                            case List.filter (\c -> c.id == collectionId) model.items |> List.head of
                                Nothing ->
                                    ( { model | status = Just "Select a collection first." }, Cmd.none, [] )

                                Just collection ->
                                    if not (List.member postId collection.postIds) then
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
                            nextItems =
                                List.map
                                    (\collection ->
                                        if collection.id == collectionId then
                                            { collection | postIds = List.filter ((/=) postId) collection.postIds }

                                        else
                                            collection
                                    )
                                    model.items

                            base =
                                { model | items = nextItems, removals = Set.remove postId model.removals, status = Just "Removed from collection." }
                        in
                        if model.browse == Just collectionId then
                            startPrefixRefresh ctx collectionId
                                { base | status = Just "Removed from collection. Refreshing…" }

                        else
                            ( base, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | removals = Set.remove postId model.removals, status = Just (errorToString error) }, Cmd.none, [] )

        RetryRequested ->
            case model.browse of
                Just collectionId ->
                    -- Retry rebuilds the browsed prefix without resetting
                    -- the workspace; a plain re-open would jump to page one.
                    startPrefixRefresh ctx collectionId { model | status = Nothing }

                Nothing ->
                    ( model, Cmd.none, [] )


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
            in
            ( refreshed, Cmd.batch [ refreshCmd, Api.Collection.list ctx.apiBase ListCompleted ], outs )

        CollectionLogic.RefreshMetadataOnly ->
            ( model, Api.Collection.list ctx.apiBase ListCompleted, [] )


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
        ( { model
            | state = Ready
            , cursor = next.cursor
            , browse = Just collectionId
            , refresh = Nothing
            , status = Nothing
          }
        , Api.Collection.list ctx.apiBase ListCompleted
        , [ RefreshLoaded { sequence = next.fetched, nextCursor = next.cursor } ]
        )


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


indexIn : String -> List String -> Maybe Int
indexIn postId ids =
    ids
        |> List.indexedMap Tuple.pair
        |> List.filter (\( _, candidate ) -> candidate == postId)
        |> List.head
        |> Maybe.map Tuple.first


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
