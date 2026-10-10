module Domain.CollectionLogic exposing
    ( CollectionFailure(..)
    , StaleMutationRecovery(..)
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
    , classifyCollectionError
    , staleMutationRecovery
    , clampScrollTop
    )

import Domain.Selection as Selection exposing (Selection)
import Domain.Sequence as Sequence exposing (Sequence)
import Http
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
with a manual retry.
-}
type CollectionFailure
    = CollectionChanged
    | InvalidCursor
    | TransportError


classifyCollectionError : Http.Error -> CollectionFailure
classifyCollectionError error =
    case error of
        Http.BadStatus 409 ->
            CollectionChanged

        Http.BadStatus 400 ->
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
