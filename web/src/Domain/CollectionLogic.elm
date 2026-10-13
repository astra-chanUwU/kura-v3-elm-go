module Domain.CollectionLogic exposing
    ( CollectionFailure(..)
    , StaleMutationRecovery(..)
    , clampManagePage
    , managePageCount
    , maxManagePages
    , planFirst
    , planLast
    , planMove
    , shouldRefresh
    , isStaleRequest
    , isStaleGeneration
    , isObsoleteResponse
    , refreshSelection
    , validModeFor
    , collectionPageSize
    , maxRefreshPages
    , maxRefreshAttempts
    , refreshTarget
    , refreshPageCount
    , shouldRetryRefresh
    , needsMoreRefreshPages
    , classifyCollectionStatus
    , staleMutationRecovery
    , clampScrollTop
    )

import Domain.Selection as Selection exposing (Selection)
import Domain.Sequence as Sequence exposing (Sequence)
import Set


shouldRefresh : Maybe String -> String -> Bool
shouldRefresh collectionBrowse affectedId =
    collectionBrowse == Just affectedId


isStaleRequest : Int -> Int -> Bool
isStaleRequest expected actual =
    expected /= actual


isStaleGeneration : Int -> Int -> Bool
isStaleGeneration expectedGeneration modelGeneration =
    expectedGeneration /= modelGeneration


{-| A collection response is obsolete when its request predates the
current one (a newer query, navigation, or refresh invalidated it) or
when the workspace no longer shows that collection. Query changes and
navigation both bump the request id and clear the browsed collection,
so one check covers stale appends, late refresh pages, and responses
that arrive after the user moved on.
-}
isObsoleteResponse : Int -> Int -> Maybe String -> String -> Bool
isObsoleteResponse expectedRequest actualRequest collectionBrowse collectionId =
    expectedRequest /= actualRequest || collectionBrowse /= Just collectionId


{-| How collection page failures recover. `CollectionChanged` (HTTP 409)
means the cursor's membership/order version moved on: re-read the
previously loaded prefix from the start. `InvalidCursor` (HTTP 400)
means the cursor itself is unusable: rebuild from the first page too,
but it is reported distinctly because a cursor should never be
malformed when echoed opaquely. Anything else keeps the old results
with a manual retry. The caller translates HTTP errors before passing
their status here.
-}
type CollectionFailure
    = CollectionChanged
    | InvalidCursor
    | TransportError


classifyCollectionStatus : Int -> CollectionFailure
classifyCollectionStatus status =
    case status of
        409 ->
            CollectionChanged

        400 ->
            InvalidCursor

        _ ->
            TransportError


{-| Members per collection page. Mirrors the server default/max limit.
-}
collectionPageSize : Int
collectionPageSize =
    60


{-| Upper bound on pages fetched by one prefix refresh. A refresh that
hits the bound finalizes with what it assembled and keeps the remaining
cursor, so the rest still loads through normal scrolling instead of
one unbounded download.
-}
maxRefreshPages : Int
maxRefreshPages =
    25


{-| Automatic restarts when the collection keeps changing under a
refresh. Past this count the workspace stops retrying and offers a
manual retry instead of looping.
-}
maxRefreshAttempts : Int
maxRefreshAttempts =
    3


{-| How many members a prefix refresh re-reads: the previously loaded
prefix, or one page when nothing (or less than a page) is loaded. The
workspace never collapses to page one and never downloads the whole
collection merely to refresh.
-}
refreshTarget : Int -> Int
refreshTarget loaded =
    max collectionPageSize loaded


{-| How many page requests that target takes, bounded by
`maxRefreshPages`.
-}
refreshPageCount : Int -> Int
refreshPageCount loaded =
    min maxRefreshPages (max 1 ((refreshTarget loaded + collectionPageSize - 1) // collectionPageSize))


{-| Whether a refresh that just hit a changed collection restarts
automatically (`attempt` counts completed restarts so far).
-}
shouldRetryRefresh : Int -> Bool
shouldRetryRefresh attempt =
    attempt < maxRefreshAttempts


{-| Whether a refresh in progress needs another page: fewer members
assembled than the target and the server offers a next cursor. A final
page (`Nothing`) stops the refresh even when the collection shrank
below the target.
-}
needsMoreRefreshPages : Int -> Int -> Maybe String -> Bool
needsMoreRefreshPages fetched target cursor =
    fetched < target && cursor /= Nothing


{-| How a settled collection mutation from an older access epoch
recovers. The mutation itself is never replayed and credentials are
untouched: public metadata always re-reads, and the affected
collection's loaded prefix rebuilds only while it is still browsed.
A 401 arriving here belongs to the old epoch, so current (possibly
freshly validated) access stands.
-}
type StaleMutationRecovery
    = RefreshBrowsedCollection
    | RefreshMetadataOnly


staleMutationRecovery : Maybe String -> String -> StaleMutationRecovery
staleMutationRecovery collectionBrowse affectedId =
    if collectionBrowse == Just affectedId then
        RefreshBrowsedCollection

    else
        RefreshMetadataOnly


{-| Overall guard on membership pages fetched for one management
window: 1000 pages (60000 rows at 60 per page). This supports a
1000-member collection across 17 pages with room to spare. The
ordering UI renders at most one 60-row page and fetches single pages
on demand; hitting this guard is reported, never a silent no-op.
-}
maxManagePages : Int
maxManagePages =
    1000


{-| How many 60-row management pages a fetched window spans.
-}
managePageCount : Int -> Int
managePageCount rowCount =
    max 1 ((rowCount + collectionPageSize - 1) // collectionPageSize)


{-| Clamp a management page index into the pages a fetched window spans.
-}
clampManagePage : Int -> Int -> Int
clampManagePage rowCount page =
    clamp 0 (managePageCount rowCount - 1) page


{-| Plan a one-step visible move inside a fetched ordering window. The
result is the `before_post_id` for the single-post move endpoint: `Just
(Just id)` inserts before that real visible neighbor, `Just Nothing`
moves to the very end, and `Nothing` means no move is possible yet —
either the post already sits at that edge, or (moving down past the
window end while more members exist) the next neighbor is unfetched.
Hidden members are server-owned: the plan names only visible neighbors
or the end, never a whole-array order.
-}
planMove : List String -> Bool -> String -> Int -> Maybe (Maybe String)
planMove ids hasMore postId delta =
    case indexOf postId ids of
        Nothing ->
            Nothing

        Just idx ->
            if delta == -1 then
                if idx == 0 then
                    Nothing

                else
                    Just (List.drop (idx - 1) ids |> List.head)

            else if delta == 1 then
                if idx + 1 >= List.length ids then
                    Nothing

                else if idx + 2 < List.length ids then
                    Just (List.drop (idx + 2) ids |> List.head)

                else if hasMore then
                    Nothing

                else
                    Just Nothing

            else
                Nothing


{-| Plan a move to the first visible position: insert before the first
fetched member, which starts at the collection's visible start. `Nothing`
when the post is already first or unknown.
-}
planFirst : List String -> String -> Maybe (Maybe String)
planFirst ids postId =
    case ( List.head ids, indexOf postId ids ) of
        ( Just first, Just idx ) ->
            if idx == 0 then
                Nothing

            else
                Just (Just first)

        _ ->
            Nothing


{-| Plan a move to the very end. `before_post_id = null` is the real end
regardless of unfetched or hidden members, so this never needs the whole
collection; it is a no-op only for the known last visible member when
nothing follows it.
-}
planLast : List String -> Bool -> String -> Maybe (Maybe String)
planLast ids hasMore postId =
    case indexOf postId ids of
        Nothing ->
            Nothing

        Just idx ->
            if idx == List.length ids - 1 && not hasMore then
                Nothing

            else
                Just Nothing


indexOf : String -> List String -> Maybe Int
indexOf postId ids =
    ids
        |> List.indexedMap Tuple.pair
        |> List.filter (\( _, candidate ) -> candidate == postId)
        |> List.head
        |> Maybe.map Tuple.first


{-| Clamp a desired grid scroll offset into the bounds of rebuilt
content: `contentHeight` is the total grid height for the new sequence
and current layout, `viewportHeight` the visible height. A valid offset
is preserved; a collection that shrank or emptied clamps down,
including to zero when nothing is scrollable.
-}
clampScrollTop : Float -> Float -> Float -> Float
clampScrollTop contentHeight viewportHeight desired =
    clamp 0 (max 0 (contentHeight - viewportHeight)) desired


refreshSelection : Sequence -> Sequence -> Selection -> Selection
refreshSelection oldSequence newSequence oldSelection =
    let
        pruned =
            Selection.prune newSequence oldSelection
    in
    case ( oldSelection.active, pruned.active ) of
        ( Just oldActive, Nothing ) ->
            if Sequence.isEmpty newSequence then
                pruned

            else
                case Sequence.indexOf oldActive oldSequence of
                    Just oldIdx ->
                        let
                            nearby =
                                Sequence.idAt oldIdx newSequence
                                    |> Maybe.withDefault
                                        (Sequence.idAt (oldIdx - 1) newSequence
                                            |> Maybe.withDefault
                                                (Sequence.idAt 0 newSequence
                                                    |> Maybe.withDefault oldActive
                                                )
                                        )
                        in
                        { pruned | active = Just nearby, anchor = Just nearby }

                    Nothing ->
                        pruned

        _ ->
            pruned


validModeFor : Sequence -> Selection -> mode -> (Sequence -> Selection -> mode -> mode) -> mode
validModeFor sequence selection mode validModeFn =
    validModeFn sequence selection mode
