module CollectionsTests exposing (allPassed, suite)

import Domain.Post exposing (PostSummary)
import Domain.Rating
import Domain.Collection exposing (CollectionAck, CollectionSummary)
import Domain.Sequence as Sequence
import Feature.Collections as Collections
import Http
import Set


ctx : Collections.Context
ctx =
    Collections.context "" (Just "tok") True 7 [ "p1", "p2" ] 120


lockedCtx : Collections.Context
lockedCtx =
    Collections.context "" Nothing False 7 [ "p1" ] 120


run : Collections.Msg -> Collections.Model -> ( Collections.Model, List Collections.OutMsg )
run msg model =
    let
        ( settled, _, outs ) =
            Collections.update ctx msg model
    in
    ( settled, outs )


summary : String -> CollectionSummary
summary id =
    { id = id, name = "Collection " ++ id, version = 3, memberCount = 3, visiblePostCount = 3 }


ack : String -> CollectionAck
ack id =
    { id = id, name = "Collection " ++ id, version = 4 }


withItems : Collections.Model
withItems =
    run (Collections.SummariesCompleted 0 (Ok [ summary "c1", summary "c2" ])) Collections.init
        |> Tuple.first


opened : Collections.Model
opened =
    run (Collections.OpenRequested "c1") withItems |> Tuple.first


page : List String -> Maybe String -> { posts : List PostSummary, nextCursor : Maybe String, collectionVersion : Int }
page ids cursor =
    { posts = List.map (\id -> { id = id, previewUrl = "/m", originalUrl = "/o", mediaType = "image/jpeg", width = 1, height = 1, tags = [], rating = Domain.Rating.Unrated, ratingVersion = Just 0 }) ids
    , nextCursor = cursor
    , collectionVersion = 3
    }


browsed : Collections.Model
browsed =
    run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Ok (page [ "p1" ] (Just "cur1")))) opened
        |> Tuple.first


caseOpenStartsLoading : Bool
caseOpenStartsLoading =
    Collections.browse opened == Just "c1"
        && Collections.state opened == Collections.Loading
        && Collections.activeId opened == Just "c1"
        && Collections.requestId opened == 1


caseObsoleteBrowseDropped : Bool
caseObsoleteBrowseDropped =
    let
        ( settled, outs ) =
            run (Collections.PostsCompleted 999 "c1" (Ok (page [ "p1" ] Nothing))) opened
    in
    outs == [] && settled == opened


caseBrowseSwitchDropsLate : Bool
caseBrowseSwitchDropsLate =
    let
        reopened =
            run (Collections.OpenRequested "c2") opened |> Tuple.first

        ( settled, outs ) =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Ok (page [ "p1" ] Nothing))) reopened
    in
    outs == [] && settled == reopened


caseBrowseSuccessLoads : Bool
caseBrowseSuccessLoads =
    let
        response =
            page [ "p1" ] (Just "cur1")

        ( settled, outs ) =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Ok response)) opened
    in
    outs == [ Collections.BrowseLoaded { posts = response.posts, nextCursor = Just "cur1" } ]
        && Collections.state settled == Collections.Ready
        && Collections.hasMore settled


caseBrowseFailureKeepsBrowse : Bool
caseBrowseFailureKeepsBrowse =
    let
        ( settled, outs ) =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Err Http.Timeout)) opened
    in
    outs == []
        && Collections.browse settled == Just "c1"
        && (case Collections.state settled of
                Collections.Failed _ ->
                    True

                _ ->
                    False
           )


caseAppendWithoutCursorNoop : Bool
caseAppendWithoutCursorNoop =
    let
        nocursor =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Ok (page [ "p1" ] Nothing))) opened
                |> Tuple.first

        ( settled, _ ) =
            ( Collections.requestMore ctx nocursor |> Tuple.first, [] )
    in
    settled == nocursor


caseAppendStartsLoadingMore : Bool
caseAppendStartsLoadingMore =
    let
        paged =
            Collections.requestMore ctx browsed |> Tuple.first
    in
    Collections.state paged == Collections.LoadingMore


caseAppendSuccessExtends : Bool
caseAppendSuccessExtends =
    let
        response =
            page [ "p2" ] (Just "cur2")

        paged =
            Collections.requestMore ctx browsed |> Tuple.first

        ( settled, outs ) =
            run (Collections.AppendCompleted (Collections.requestId browsed) "c1" (Ok response)) paged
    in
    outs == [ Collections.AppendLoaded { posts = response.posts, nextCursor = Just "cur2" } ]
        && Collections.state settled == Collections.Ready
        && Collections.hasMore settled


caseAppendConflictRefreshes : Bool
caseAppendConflictRefreshes =
    let
        paged =
            Collections.requestMore ctx browsed |> Tuple.first

        ( settled, outs ) =
            run (Collections.AppendCompleted (Collections.requestId browsed) "c1" (Err (Http.BadStatus 409))) paged
    in
    outs == []
        && Collections.state settled == Collections.LoadingMore
        && Collections.status settled == Just "The collection changed. Refreshing…"


caseAppendBadCursorReloads : Bool
caseAppendBadCursorReloads =
    let
        paged =
            Collections.requestMore ctx browsed |> Tuple.first

        ( settled, _ ) =
            run (Collections.AppendCompleted (Collections.requestId browsed) "c1" (Err (Http.BadStatus 400))) paged
    in
    Collections.status settled == Just "The page cursor was invalid. Reloading…"


caseAppendTransportFails : Bool
caseAppendTransportFails =
    let
        paged =
            Collections.requestMore ctx browsed |> Tuple.first

        ( settled, outs ) =
            run (Collections.AppendCompleted (Collections.requestId browsed) "c1" (Err Http.Timeout)) paged
    in
    outs == []
        && (case Collections.state settled of
                Collections.Failed _ ->
                    True

                _ ->
                    False
           )
        && Collections.status settled == Just "The search timed out."


caseRefreshChainsPages : Bool
caseRefreshChainsPages =
    let
        added =
            run (Collections.Added 7 "c1" (Ok (ack "c1"))) browsed |> Tuple.first

        ( chained, outs ) =
            run (Collections.RefreshCompleted (Collections.requestId added) "c1" (Ok (page [ "p1" ] (Just "cur1")))) added
    in
    outs == [] && Collections.state chained == Collections.LoadingMore


caseRefreshFinalizesWindow : Bool
caseRefreshFinalizesWindow =
    let
        added =
            run (Collections.Added 7 "c1" (Ok (ack "c1"))) browsed |> Tuple.first

        ( settled, outs ) =
            run (Collections.RefreshCompleted (Collections.requestId added) "c1" (Ok (page [ "p0", "p1", "p2" ] Nothing))) added
    in
    case outs of
        [ Collections.RefreshLoaded arrival ] ->
            Collections.state settled == Collections.Ready
                && Collections.status settled == Nothing
                && Sequence.length arrival.sequence == 3
                && arrival.nextCursor == Nothing

        _ ->
            False


caseRefreshRestartsBounded : Bool
caseRefreshRestartsBounded =
    let
        added =
            run (Collections.Added 7 "c1" (Ok (ack "c1"))) browsed |> Tuple.first

        ( restarted, _ ) =
            run (Collections.RefreshCompleted (Collections.requestId added) "c1" (Err (Http.BadStatus 409))) added
    in
    Collections.state restarted == Collections.LoadingMore
        && Collections.status restarted == Just "The collection changed again. Retrying…"


caseStaleMutationRefreshesBrowsed : Bool
caseStaleMutationRefreshesBrowsed =
    let
        ( settled, outs ) =
            run (Collections.Added 3 "c1" (Ok (ack "c1"))) browsed
    in
    outs == [] && Collections.state settled == Collections.LoadingMore


caseStaleMutationListsOnly : Bool
caseStaleMutationListsOnly =
    let
        unbrowsed =
            Collections.abandonBrowse browsed

        ( settled, outs ) =
            run (Collections.Added 3 "c1" (Ok (ack "c1"))) unbrowsed
    in
    outs == []
        && Collections.browse settled == Nothing
        && Collections.state settled == Collections.Idle
        && Collections.items settled == Collections.items unbrowsed


caseStaleRemovalClearsMarker : Bool
caseStaleRemovalClearsMarker =
    let
        marked =
            run (Collections.RemoveRequested "p1")
                { withItems | activeId = Just "c1" }
                |> Tuple.first

        ( settled, _ ) =
            run (Collections.RemoveCompleted 3 "c1" "p1" (Ok ())) marked
    in
    Collections.removals settled == Set.empty


caseMutationUnauthorizedRevokes : Bool
caseMutationUnauthorizedRevokes =
    let
        ( _, outs ) =
            run (Collections.Added 7 "c1" (Err (Http.BadStatus 401))) browsed
    in
    outs == [ Collections.AccessRevoked ]


caseAddedRefreshesWhileBrowsed : Bool
caseAddedRefreshesWhileBrowsed =
    let
        ( settled, outs ) =
            run (Collections.Added 7 "c1" (Ok (ack "c1"))) browsed
    in
    outs == []
        && Collections.state settled == Collections.LoadingMore
        && Collections.status settled == Just "Posts added to collection. Refreshing…"


caseAddedListsWhenAway : Bool
caseAddedListsWhenAway =
    let
        unbrowsed =
            Collections.abandonBrowse browsed

        ( settled, outs ) =
            run (Collections.Added 7 "c1" (Ok (ack "c1"))) unbrowsed
    in
    outs == []
        && Collections.status settled == Just "Posts added to collection."
        && List.any (\c -> c.id == "c1") (Collections.items settled)


caseAddedWrongResourceRejected : Bool
caseAddedWrongResourceRejected =
    let
        ( settled, outs ) =
            run (Collections.Added 7 "c1" (Ok (ack "c2"))) browsed
    in
    outs == []
        && Collections.status settled == Just "The add response was invalid."
        && Collections.state settled /= Collections.LoadingMore


caseCreateGuards : Bool
caseCreateGuards =
    let
        ( blank, blankOuts ) =
            run (Collections.CreateRequested "   ") withItems

        ( locked, _ ) =
            Collections.update lockedCtx (Collections.CreateRequested "new") withItems
                |> (\( m, _, o ) -> ( m, o ))
    in
    blank == withItems
        && blankOuts == []
        && Collections.status locked == Just "Unlock to create collections."


caseCreateSuccessActivates : Bool
caseCreateSuccessActivates =
    let
        ( creating, _ ) =
            run (Collections.CreateRequested "fresh") withItems

        ( settled, outs ) =
            run (Collections.Created 7 (Ok { id = "c9", name = "fresh", version = 0 })) creating
    in
    outs == []
        && Collections.activeId settled == Just "c9"
        && Collections.draft settled == ""
        && Collections.status settled == Just "Collection created."
        && List.any (\c -> c.id == "c9" && c.version == 0 && c.memberCount == 0) (Collections.items settled)


caseCreateStaleListsOnly : Bool
caseCreateStaleListsOnly =
    let
        ( creating, _ ) =
            run (Collections.CreateRequested "fresh") withItems

        ( settled, outs ) =
            run (Collections.Created 3 (Ok (ack "c9"))) creating
    in
    outs == []
        && Collections.items settled == Collections.items creating
        && Collections.activeId settled == Collections.activeId creating


caseMoveGuards : Bool
caseMoveGuards =
    let
        ( locked, _ ) =
            Collections.update lockedCtx (Collections.MoveRequested "p1" 1) withItems
                |> (\( m, _, o ) -> ( m, o ))

        ( unknown, _ ) =
            run (Collections.MoveRequested "p1" 1) Collections.init

        ( unmanaged, _ ) =
            run (Collections.MoveRequested "p1" -1) { withItems | activeId = Just "c1" }
    in
    Collections.status locked == Just "Unlock to reorder collections."
        && Collections.status unknown == Just "Select a collection first."
        && Collections.status unmanaged == Just "Open ordering to move posts."


caseRemoveGuards : Bool
caseRemoveGuards =
    let
        active =
            { withItems | activeId = Just "c1" }

        ( duplicate, _ ) =
            run (Collections.RemoveRequested "p1") { active | removals = Set.singleton "p1" }

        ( marked, _ ) =
            run (Collections.RemoveRequested "p1") active
    in
    duplicate == { active | removals = Set.singleton "p1" }
        && Collections.removals marked == Set.singleton "p1"
        && Collections.status marked == Just "Removing…"


caseRemoveSuccessClearsMarker : Bool
caseRemoveSuccessClearsMarker =
    let
        active =
            { withItems | activeId = Just "c1" }

        ( marked, _ ) =
            run (Collections.RemoveRequested "p1") active

        ( settled, outs ) =
            run (Collections.RemoveCompleted 7 "c1" "p1" (Ok ())) marked
    in
    outs == []
        && Collections.removals settled == Set.empty
        && Collections.status settled == Just "Removed from collection."
        && List.any (\c -> c.id == "c1") (Collections.items settled)


caseRetryNeedsBrowse : Bool
caseRetryNeedsBrowse =
    let
        ( idle, idleOuts ) =
            run Collections.RetryRequested Collections.init

        ( refreshing, _ ) =
            run Collections.RetryRequested browsed
    in
    idleOuts == []
        && idle == Collections.init
        && Collections.state refreshing == Collections.LoadingMore


caseAbandonKeepsList : Bool
caseAbandonKeepsList =
    let
        abandoned =
            Collections.abandonBrowse browsed
    in
    Collections.browse abandoned == Nothing
        && Collections.state abandoned == Collections.Idle
        && Collections.items abandoned == Collections.items browsed
        && Collections.activeId abandoned == Collections.activeId browsed


caseAbandonDropsLatePage : Bool
caseAbandonDropsLatePage =
    let
        abandoned =
            Collections.abandonBrowse opened

        ( settled, outs ) =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Ok (page [ "p9" ] Nothing))) abandoned
    in
    Collections.requestId abandoned /= Collections.requestId opened
        && outs == []
        && settled == abandoned


caseMissingCollection : Bool
caseMissingCollection =
    let
        ( settled, outs ) =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Err (Http.BadStatus 404))) opened
    in
    outs == []
        && Collections.browse settled == Just "c1"
        && Collections.state settled == Collections.Failed "This collection is missing or was deleted."
        && Collections.status settled == Just "This collection is missing or was deleted."


caseRevokedCollection : Bool
caseRevokedCollection =
    let
        ( settled, _ ) =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Err (Http.BadStatus 403))) opened
    in
    Collections.state settled == Collections.Failed "You no longer have access to this collection."
        && Collections.status settled == Just "You no longer have access to this collection."


caseSignedOutCollection : Bool
caseSignedOutCollection =
    let
        ( settled, _ ) =
            run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Err (Http.BadStatus 401))) opened
    in
    Collections.state settled == Collections.Failed "Sign in to open this collection."
        && Collections.browse settled == Just "c1"


caseLockedKeepsBrowse : Bool
caseLockedKeepsBrowse =
    let
        flagged =
            { browsed | status = Just "old", removals = Set.singleton "p1" }

        cleared =
            Collections.locked flagged
    in
    Collections.status cleared == Nothing
        && Collections.removals cleared == Set.empty
        && Collections.browse cleared == Just "c1"


caseDraftClearsNotice : Bool
caseDraftClearsNotice =
    let
        flagged =
            { withItems | status = Just "old" }

        ( settled, _ ) =
            run (Collections.DraftChanged "new") flagged
    in
    Collections.draft settled == "new" && Collections.status settled == Nothing


caseSelectActivates : Bool
caseSelectActivates =
    let
        ( settled, _ ) =
            run (Collections.Selected "c2") { withItems | status = Just "old" }
    in
    Collections.activeId settled == Just "c2" && Collections.status settled == Nothing


caseSummariesKeepActive : Bool
caseSummariesKeepActive =
    let
        fresh =
            Collections.init

        current =
            { fresh | activeId = Just "c2" }

        ( settled, _ ) =
            run (Collections.SummariesCompleted 0 (Ok [ summary "c1", summary "c2" ])) current

        ( fallen, _ ) =
            run (Collections.SummariesCompleted 0 (Ok [ summary "c1" ])) current

        ( failed, _ ) =
            run (Collections.SummariesCompleted 0 (Err Http.Timeout)) current
    in
    Collections.activeId settled == Just "c2"
        && Collections.activeId fallen == Just "c1"
        && Collections.summariesUnavailable failed == Just "The search timed out."
        && Collections.status failed == Just "Collections are unavailable."
        && Collections.items failed == []


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "open starts loading the collection", caseOpenStartsLoading )
    , ( "obsolete browse response dropped", caseObsoleteBrowseDropped )
    , ( "browse switch drops the late page", caseBrowseSwitchDropsLate )
    , ( "browse success reports the page", caseBrowseSuccessLoads )
    , ( "browse failure keeps browsing", caseBrowseFailureKeepsBrowse )
    , ( "append without cursor is a no-op", caseAppendWithoutCursorNoop )
    , ( "append with cursor loads more", caseAppendStartsLoadingMore )
    , ( "append success extends the page", caseAppendSuccessExtends )
    , ( "append conflict restarts the prefix", caseAppendConflictRefreshes )
    , ( "append bad cursor reloads", caseAppendBadCursorReloads )
    , ( "append transport failure keeps results", caseAppendTransportFails )
    , ( "refresh chains bounded pages", caseRefreshChainsPages )
    , ( "refresh finalizes the window", caseRefreshFinalizesWindow )
    , ( "refresh restarts after conflicts", caseRefreshRestartsBounded )
    , ( "stale mutation refreshes the browsed set", caseStaleMutationRefreshesBrowsed )
    , ( "stale mutation elsewhere lists only", caseStaleMutationListsOnly )
    , ( "stale removal clears its marker", caseStaleRemovalClearsMarker )
    , ( "mutation 401 revokes access", caseMutationUnauthorizedRevokes )
    , ( "added posts refresh while browsed", caseAddedRefreshesWhileBrowsed )
    , ( "added posts list when away", caseAddedListsWhenAway )
    , ( "added wrong-resource response rejected", caseAddedWrongResourceRejected )
    , ( "create guards name and access", caseCreateGuards )
    , ( "create success activates", caseCreateSuccessActivates )
    , ( "stale create lists only", caseCreateStaleListsOnly )
    , ( "move guards access and selection", caseMoveGuards )
    , ( "remove guards duplicates", caseRemoveGuards )
    , ( "remove success clears the marker", caseRemoveSuccessClearsMarker )
    , ( "retry needs a browsed collection", caseRetryNeedsBrowse )
    , ( "abandon keeps the list", caseAbandonKeepsList )
    , ( "abandon drops a late page", caseAbandonDropsLatePage )
    , ( "missing collection stays browsable as an error", caseMissingCollection )
    , ( "revoked collection names the denial", caseRevokedCollection )
    , ( "signed-out collection asks for sign-in", caseSignedOutCollection )
    , ( "lock keeps browsing", caseLockedKeepsBrowse )
    , ( "draft edits clear the notice", caseDraftClearsNotice )
    , ( "select activates", caseSelectActivates )
    , ( "summaries keep the active collection", caseSummariesKeepActive )
    ]
