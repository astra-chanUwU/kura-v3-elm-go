module CollectionBoundedTests exposing (allPassed, suite)

import Domain.Post exposing (PostSummary)
import Domain.Rating
import Domain.Collection as Collection
import Domain.CollectionLogic as Logic
import Feature.Collections as Collections
import Http
import Json.Decode as Decode
import Set


ctx : Collections.Context
ctx =
    Collections.context "" (Just "tok") True 7 [ "p1" ] 120


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


runLocked : Collections.Msg -> Collections.Model -> ( Collections.Model, List Collections.OutMsg )
runLocked msg model =
    let
        ( settled, _, outs ) =
            Collections.update lockedCtx msg model
    in
    ( settled, outs )


summary : String -> Int -> Collection.CollectionSummary
summary id version =
    { id = id, name = "Collection " ++ id, version = version, memberCount = 3, visiblePostCount = 3 }


withSummaries : Collections.Model
withSummaries =
    run (Collections.SummariesCompleted 0 (Ok [ summary "c1" 5, summary "c2" 2 ])) Collections.init
        |> Tuple.first


post : String -> PostSummary
post id =
    { id = id, previewUrl = "/m", originalUrl = "/o", mediaType = "image/jpeg", width = 1, height = 1, tags = [], rating = Domain.Rating.Unrated, ratingVersion = Just 0 }


pageOf : List String -> Maybe String -> Int -> Collection.CollectionPostsPage
pageOf pageIds cursor version =
    { posts = List.map post pageIds, nextCursor = cursor, collectionVersion = version }


ids : Int -> Int -> List String
ids start count =
    List.range start (start + count - 1) |> List.map (\n -> "p" ++ String.fromInt n)


{-| Open ordering for c1 and feed it one page. -}
managed : List String -> Maybe String -> Collections.Model
managed pageIds cursor =
    let
        opened =
            run (Collections.ManageRequested "c1") withSummaries |> Tuple.first
    in
    run (Collections.ManageCompleted opened.manageRequestId "c1" (Ok (pageOf pageIds cursor 5))) opened
        |> Tuple.first


browsedModel : Collections.Model
browsedModel =
    let
        opened =
            run (Collections.OpenRequested "c1") withSummaries |> Tuple.first
    in
    run (Collections.PostsCompleted (Collections.requestId opened) "c1" (Ok (pageOf [ "p1", "p2", "p3" ] Nothing 5))) opened
        |> Tuple.first


managedBrowsed : Collections.Model
managedBrowsed =
    let
        opened =
            run (Collections.ManageRequested "c1") browsedModel |> Tuple.first
    in
    run (Collections.ManageCompleted opened.manageRequestId "c1" (Ok (pageOf [ "p1", "p2", "p3" ] Nothing 5))) opened
        |> Tuple.first



-- Summary / move / management decoding


caseSummaryDecodes : Bool
caseSummaryDecodes =
    Decode.decodeString Collection.summaryDecoder
        """{"id":"7","name":"Fav","version":0,"member_count":1000,"visible_post_count":998}"""
        == Ok { id = "7", name = "Fav", version = 0, memberCount = 1000, visiblePostCount = 998 }


caseSummaryMissingFieldFails : Bool
caseSummaryMissingFieldFails =
    Decode.decodeString Collection.summaryDecoder
        """{"id":"7","name":"Fav","version":0,"member_count":1000}"""
        |> Result.toMaybe
        |> (==) Nothing


caseSummaryNegativeFails : Bool
caseSummaryNegativeFails =
    Decode.decodeString Collection.summaryDecoder
        """{"id":"7","name":"Fav","version":0,"member_count":-1,"visible_post_count":0}"""
        |> Result.toMaybe
        |> (==) Nothing


caseLegacyPayloadIsNotSummary : Bool
caseLegacyPayloadIsNotSummary =
    Decode.decodeString Collection.summaryDecoder
        """{"id":"7","name":"Fav","post_ids":["1","2","3"]}"""
        |> Result.toMaybe
        |> (==) Nothing


caseSummariesWrapperRequired : Bool
caseSummariesWrapperRequired =
    Decode.decodeString Collection.summariesDecoder
        """[{"id":"7","name":"Fav","version":0,"member_count":0,"visible_post_count":0}]"""
        |> Result.toMaybe
        |> (==) Nothing


caseMoveDecodes : Bool
caseMoveDecodes =
    Decode.decodeString Collection.moveDecoder
        """{"id":"7","collection_id":"7","version":4,"changed":true}"""
        == Ok { id = "7", collectionId = "7", version = 4, changed = True }


caseMoveMissingCollectionFails : Bool
caseMoveMissingCollectionFails =
    Decode.decodeString Collection.moveDecoder
        """{"id":"9","version":6,"changed":true}"""
        |> Result.toMaybe
        |> (==) Nothing


caseSummaryIsNotMove : Bool
caseSummaryIsNotMove =
    Decode.decodeString Collection.moveDecoder
        """{"id":"7","name":"Fav","version":0,"member_count":1,"visible_post_count":1}"""
        |> Result.toMaybe
        |> (==) Nothing


caseManageDecodes : Bool
caseManageDecodes =
    Decode.decodeString Collection.manageDecoder
        """{"id":"7","name":"New","version":6,"changed":false}"""
        == Ok { id = "7", name = "New", version = 6, changed = False }


caseMoveIsNotManage : Bool
caseMoveIsNotManage =
    Decode.decodeString Collection.manageDecoder
        """{"id":"9","collection_id":"7","version":6,"changed":true}"""
        |> Result.toMaybe
        |> (==) Nothing


caseAckIgnoresMemberArray : Bool
caseAckIgnoresMemberArray =
    Decode.decodeString Collection.ackDecoder
        """{"id":"7","name":"Fav","version":2,"post_ids":["1","2","3"]}"""
        == Ok { id = "7", name = "Fav", version = 2 }



-- Move planning over a visible window


window : List String
window =
    [ "a", "b", "c", "d" ]


casePlanUpUsesPredecessor : Bool
casePlanUpUsesPredecessor =
    Logic.planMove window False "c" -1 == Just (Just "b")


casePlanDownUsesFollowingNeighbor : Bool
casePlanDownUsesFollowingNeighbor =
    Logic.planMove window False "b" 1 == Just (Just "d")


casePlanDownToEnd : Bool
casePlanDownToEnd =
    Logic.planMove window False "c" 1 == Just Nothing


casePlanDownNeedsNextPage : Bool
casePlanDownNeedsNextPage =
    Logic.planMove window True "c" 1 == Nothing


casePlanEdgesAreNoops : Bool
casePlanEdgesAreNoops =
    Logic.planMove window False "a" -1 == Nothing
        && Logic.planMove window False "d" 1 == Nothing
        && Logic.planMove window True "d" 1 == Nothing


casePlanUnknownAndBadDelta : Bool
casePlanUnknownAndBadDelta =
    Logic.planMove window False "zzz" 1 == Nothing
        && Logic.planMove window False "b" 2 == Nothing


casePlanFirst : Bool
casePlanFirst =
    Logic.planFirst window "c" == Just (Just "a")
        && Logic.planFirst window "a" == Nothing


casePlanLast : Bool
casePlanLast =
    Logic.planLast window False "b" == Just Nothing
        && Logic.planLast window False "d" == Nothing
        && Logic.planLast window True "d" == Just Nothing


caseManagePageBounds : Bool
caseManagePageBounds =
    Logic.managePageCount 0 == 1
        && Logic.managePageCount 60 == 1
        && Logic.managePageCount 61 == 2
        && Logic.managePageCount 600 == 10
        && Logic.clampManagePage 61 9 == 1
        && Logic.clampManagePage 61 -2 == 0



-- Bounded management navigation


caseManageRendersOnePage : Bool
caseManageRendersOnePage =
    case Collections.manageView (managed (ids 1 60) (Just "cur")) of
        Just view ->
            List.length view.rows == 60
                && view.page == 0
                && view.pageCount == 1
                && view.hasNext
                && not view.hasPrev
                && (view.rows |> List.head |> Maybe.map .canUp) == Just False

        Nothing ->
            False


caseManageNextFetchesThenAdvances : Bool
caseManageNextFetchesThenAdvances =
    let
        first =
            managed (ids 1 60) (Just "cur")

        fetching =
            run Collections.ManageNextRequested first |> Tuple.first

        arrived =
            run (Collections.ManageCompleted fetching.manageRequestId "c1" (Ok (pageOf (ids 61 30) Nothing 5))) fetching
                |> Tuple.first
    in
    case Collections.manageView arrived of
        Just view ->
            Collections.manageView fetching
                |> Maybe.map .loading
                |> (==) (Just True)
                |> (\loading -> loading && view.page == 1 && List.length view.rows == 30 && view.pageCount == 2 && view.hasPrev && not view.hasNext)

        Nothing ->
            False


caseManageBackReturns : Bool
caseManageBackReturns =
    let
        first =
            managed (ids 1 60) (Just "cur")

        fetching =
            run Collections.ManageNextRequested first |> Tuple.first

        second =
            run (Collections.ManageCompleted fetching.manageRequestId "c1" (Ok (pageOf (ids 61 30) Nothing 5))) fetching
                |> Tuple.first

        back =
            run Collections.ManageBackRequested second |> Tuple.first
    in
    Collections.manageView back
        |> Maybe.map (\view -> view.page == 0 && List.length view.rows == 60)
        |> (==) (Just True)


caseManageFetchBound : Bool
caseManageFetchBound =
    let
        feedPage n model =
            let
                fetching =
                    run Collections.ManageNextRequested model |> Tuple.first
            in
            run (Collections.ManageCompleted fetching.manageRequestId "c1" (Ok (pageOf (ids (n * 60 + 1) 60) (Just "cur") 5))) fetching
                |> Tuple.first

        filled =
            List.foldl feedPage (managed (ids 1 60) (Just "cur")) (List.range 1 12)
    in
    Collections.manageView filled
        |> Maybe.map (\view -> view.pageCount == 13 && not view.loading)
        |> (==) (Just True)


caseCrossPageUpMoveFires : Bool
caseCrossPageUpMoveFires =
    let
        first =
            managed (ids 1 60) (Just "cur")

        fetching =
            run Collections.ManageNextRequested first |> Tuple.first

        second =
            run (Collections.ManageCompleted fetching.manageRequestId "c1" (Ok (pageOf (ids 61 30) Nothing 5))) fetching
                |> Tuple.first

        ( moved, _ ) =
            run (Collections.MoveRequested "p61" -1) second
    in
    Collections.status moved == Just "Reordering…"
        && Collections.manageBusy moved


caseFirstAndLastMovesFire : Bool
caseFirstAndLastMovesFire =
    let
        model =
            managed [ "a", "b", "c" ] Nothing

        ( toFirst, _ ) =
            run (Collections.MoveFirstRequested "c") model

        ( toLast, _ ) =
            run (Collections.MoveLastRequested "a") model

        ( alreadyFirst, _ ) =
            run (Collections.MoveFirstRequested "a") model
    in
    Collections.status toFirst == Just "Reordering…"
        && Collections.status toLast == Just "Reordering…"
        && Collections.status alreadyFirst == Just "Already at that edge — no move needed."


caseDownAtWindowEndFetchesNeighbor : Bool
caseDownAtWindowEndFetchesNeighbor =
    let
        model =
            managed (ids 1 60) (Just "cur")

        ( fetching, _ ) =
            run (Collections.MoveRequested "p60" 1) model
    in
    Collections.status fetching == Just "Loading more ordering rows…"
        && (Collections.manageView fetching |> Maybe.map .loading) == Just True


casePendingDownMoveResumes : Bool
casePendingDownMoveResumes =
    let
        model =
            managed (ids 1 60) (Just "cur")

        fetching =
            run (Collections.MoveRequested "p60" 1) model |> Tuple.first

        ( resumed, _ ) =
            run (Collections.ManageCompleted fetching.manageRequestId "c1" (Ok (pageOf (ids 61 60) (Just "cur2") 5))) fetching
    in
    Collections.status resumed == Just "Reordering…"
        && Collections.manageBusy resumed


caseConcurrentMoveIgnored : Bool
caseConcurrentMoveIgnored =
    let
        model =
            managed [ "a", "b", "c" ] Nothing

        moving =
            run (Collections.MoveRequested "a" 1) model |> Tuple.first

        ( second, outs ) =
            run (Collections.MoveRequested "b" 1) moving
    in
    outs == [] && second == moving


caseMoveWhileLoadingIgnored : Bool
caseMoveWhileLoadingIgnored =
    let
        opened =
            run (Collections.ManageRequested "c1") withSummaries |> Tuple.first

        ( settled, outs ) =
            run (Collections.MoveRequested "p1" 1) opened
    in
    outs == [] && settled == opened



-- Move completion: success, wrong resource, conflicts, navigation, lock


moveResult : String -> String -> Int -> Collection.MoveResult
moveResult _ collectionId version =
    { id = collectionId, collectionId = collectionId, version = version, changed = True }


caseMoveSuccessUpdatesVersion : Bool
caseMoveSuccessUpdatesVersion =
    let
        model =
            managed [ "a", "b", "c" ] Nothing

        moving =
            run (Collections.MoveRequested "a" 1) model |> Tuple.first

        ( settled, outs ) =
            run (Collections.MoveCompleted 7 "c1" "a" (Ok (moveResult "a" "c1" 6))) moving
    in
    outs == []
        && Collections.status settled == Just "Order saved."
        && List.any (\c -> c.id == "c1" && c.version == 6) (Collections.items settled)
        && not (Collections.manageBusy settled)


caseMoveWrongResourceRejected : Bool
caseMoveWrongResourceRejected =
    let
        model =
            managed [ "a", "b", "c" ] Nothing

        moving =
            run (Collections.MoveRequested "a" 1) model |> Tuple.first

        ( settled, outs ) =
            run (Collections.MoveCompleted 7 "c1" "a" (Ok (moveResult "a" "c2" 6))) moving
    in
    outs == []
        && Collections.status settled == Just "The move response was invalid."
        && List.any (\c -> c.id == "c1" && c.version == 5) (Collections.items settled)


caseMoveConflictRefreshesBrowsed : Bool
caseMoveConflictRefreshesBrowsed =
    let
        moving =
            run (Collections.MoveRequested "p1" 1) managedBrowsed |> Tuple.first

        ( settled, outs ) =
            run (Collections.MoveCompleted 7 "c1" "p1" (Err (Http.BadStatus 409))) moving
    in
    outs == []
        && Collections.state settled == Collections.LoadingMore
        && Collections.status settled == Just "The collection changed. Refreshing…"
        && Collections.browse settled == Just "c1"


caseNavigationBeforeMoveCompletion : Bool
caseNavigationBeforeMoveCompletion =
    let
        moving =
            run (Collections.MoveRequested "p1" 1) managedBrowsed |> Tuple.first

        navigated =
            run (Collections.OpenRequested "c2") moving |> Tuple.first

        ( settled, outs ) =
            run (Collections.MoveCompleted 7 "c1" "p1" (Ok (moveResult "p1" "c1" 6))) navigated
    in
    outs == []
        && Collections.browse settled == Just "c2"
        && Collections.state settled == Collections.Loading
        && List.any (\c -> c.id == "c1" && c.version == 6) (Collections.items settled)


caseStaleMoveCompletionReconcilesOnly : Bool
caseStaleMoveCompletionReconcilesOnly =
    let
        model =
            managed [ "a", "b", "c" ] Nothing

        moving =
            run (Collections.MoveRequested "a" 1) model |> Tuple.first

        ( settled, outs ) =
            run (Collections.MoveCompleted 3 "c1" "a" (Ok (moveResult "a" "c1" 6))) moving
    in
    outs == []
        && not (Collections.manageBusy settled)
        && Collections.state settled == Collections.Idle


caseMoveUnauthorizedRevokes : Bool
caseMoveUnauthorizedRevokes =
    let
        model =
            managed [ "a", "b", "c" ] Nothing

        moving =
            run (Collections.MoveRequested "a" 1) model |> Tuple.first

        ( _, outs ) =
            run (Collections.MoveCompleted 7 "c1" "a" (Err (Http.BadStatus 401))) moving
    in
    outs == [ Collections.AccessRevoked ]


caseLockedMoveDisabledButReadsWork : Bool
caseLockedMoveDisabledButReadsWork =
    let
        model =
            managed [ "a", "b", "c" ] Nothing

        ( lockedMove, _ ) =
            runLocked (Collections.MoveRequested "a" 1) model

        ( lockedBrowse, _ ) =
            runLocked (Collections.OpenRequested "c1") model
    in
    Collections.status lockedMove == Just "Unlock to reorder collections."
        && not (Collections.manageBusy lockedMove)
        && Collections.browse lockedBrowse == Just "c1"
        && Collections.state lockedBrowse == Collections.Loading



-- Summaries availability


caseSummariesUnavailable : Bool
caseSummariesUnavailable =
    let
        ( settled, outs ) =
            run (Collections.SummariesCompleted 0 (Err (Http.BadStatus 503))) Collections.init
    in
    outs == []
        && Collections.summariesUnavailable settled == Just "Search failed (HTTP 503)."
        && Collections.items settled == []


caseSummariesRetryRecovers : Bool
caseSummariesRetryRecovers =
    let
        failed =
            run (Collections.SummariesCompleted 0 (Err (Http.BadStatus 503))) Collections.init
                |> Tuple.first

        retrying =
            run Collections.SummariesRetryRequested failed |> Tuple.first

        ( settled, _ ) =
            run (Collections.SummariesCompleted retrying.summariesRequestId (Ok [ summary "c1" 0 ])) retrying
    in
    Collections.summariesState retrying == Collections.SummariesLoading
        && Collections.summariesUnavailable settled == Nothing
        && List.any (\c -> c.id == "c1" && c.version == 0) (Collections.items settled)


caseStaleSummariesDropped : Bool
caseStaleSummariesDropped =
    let
        ( settled, outs ) =
            run (Collections.SummariesCompleted 99 (Ok [ summary "c1" 0 ])) Collections.init
    in
    outs == [] && settled == Collections.init



-- Rename


renaming : Collections.Model
renaming =
    run (Collections.RenameRequested "c1") withSummaries |> Tuple.first


caseRenameNoopSameName : Bool
caseRenameNoopSameName =
    let
        ( settled, outs ) =
            run Collections.RenameSubmitted renaming
    in
    outs == []
        && Collections.status settled == Just "Name unchanged."
        && Collections.renamingId settled == Nothing
        && not (Collections.manageBusy settled)


caseRenameMissingState : Bool
caseRenameMissingState =
    let
        ( unknown, unknownOuts ) =
            run (Collections.RenameRequested "nope") withSummaries

        ( unsubmitted, _ ) =
            run Collections.RenameSubmitted withSummaries
    in
    unknownOuts == []
        && unknown == withSummaries
        && unsubmitted == withSummaries


caseRenameSuccess : Bool
caseRenameSuccess =
    let
        drafted =
            run (Collections.RenameDraftChanged "Renamed") renaming |> Tuple.first

        submitting =
            run Collections.RenameSubmitted drafted |> Tuple.first

        ( settled, outs ) =
            run (Collections.Renamed 7 "c1" (Ok { id = "c1", name = "Renamed", version = 6, changed = True })) submitting
    in
    Collections.status submitting == Just "Renaming…"
        && outs == []
        && Collections.renamingId settled == Nothing
        && List.any (\c -> c.id == "c1" && c.name == "Renamed" && c.version == 6) (Collections.items settled)


caseRenameZeroVersionFires : Bool
caseRenameZeroVersionFires =
    let
        zeroed =
            run (Collections.SummariesCompleted 0 (Ok [ summary "c1" 0 ])) Collections.init
                |> Tuple.first

        opened =
            run (Collections.RenameRequested "c1") zeroed |> Tuple.first

        drafted =
            run (Collections.RenameDraftChanged "Zero") opened |> Tuple.first

        ( submitting, _ ) =
            run Collections.RenameSubmitted drafted
    in
    Collections.status submitting == Just "Renaming…"


caseRenameWrongResourceRejected : Bool
caseRenameWrongResourceRejected =
    let
        drafted =
            run (Collections.RenameDraftChanged "Renamed") renaming |> Tuple.first

        submitting =
            run Collections.RenameSubmitted drafted |> Tuple.first

        ( settled, _ ) =
            run (Collections.Renamed 7 "c1" (Ok { id = "c2", name = "Renamed", version = 6, changed = True })) submitting
    in
    Collections.status settled == Just "The rename response was invalid."
        && List.any (\c -> c.id == "c1" && c.name == "Collection c1") (Collections.items settled)


caseRenameConflictKeepsBrowsing : Bool
caseRenameConflictKeepsBrowsing =
    let
        opened =
            run (Collections.RenameRequested "c1") browsedModel |> Tuple.first

        drafted =
            run (Collections.RenameDraftChanged "Renamed") opened |> Tuple.first

        submitting =
            run Collections.RenameSubmitted drafted |> Tuple.first

        ( settled, outs ) =
            run (Collections.Renamed 7 "c1" (Err (Http.BadStatus 409))) submitting
    in
    outs == []
        && Collections.browse settled == Just "c1"
        && Collections.status settled == Just "The collection changed. Refreshing…"


caseLockedRenameBlocked : Bool
caseLockedRenameBlocked =
    let
        ( settled, _ ) =
            runLocked Collections.RenameSubmitted renaming
    in
    Collections.status settled == Just "Unlock to rename collections."
        && List.any (\c -> c.id == "c1" && c.name == "Collection c1") (Collections.items settled)



-- Delete: confirmation first, posts untouched, browsing preserved on failure


caseDeleteNeedsConfirmation : Bool
caseDeleteNeedsConfirmation =
    let
        ( asked, outs ) =
            run (Collections.DeleteRequested "c1") withSummaries

        ( cancelled, _ ) =
            run Collections.DeleteCancelled asked
    in
    outs == []
        && Collections.deleteConfirmId asked == Just "c1"
        && List.length (Collections.items asked) == 2
        && not (Collections.manageBusy asked)
        && Collections.deleteConfirmId cancelled == Nothing


caseDeleteMissingState : Bool
caseDeleteMissingState =
    let
        ( unknown, _ ) =
            run (Collections.DeleteRequested "nope") withSummaries

        ( unconfirmed, _ ) =
            run Collections.DeleteConfirmed withSummaries
    in
    unknown == withSummaries && unconfirmed == withSummaries


caseDeleteSuccessAwayFromBrowse : Bool
caseDeleteSuccessAwayFromBrowse =
    let
        asked =
            run (Collections.DeleteRequested "c1") withSummaries |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 7 "c1" (Ok { id = "c1", name = "Collection c1", version = 5, changed = True })) confirming
    in
    Collections.status confirming == Just "Deleting collection…"
        && outs == []
        && List.map .id (Collections.items settled) == [ "c2" ]
        && Collections.activeId settled == Just "c2"
        && Collections.status settled == Just "Collection deleted. Its posts were not deleted."


caseDeleteBrowsedCollectionEndsBrowse : Bool
caseDeleteBrowsedCollectionEndsBrowse =
    let
        asked =
            run (Collections.DeleteRequested "c1") browsedModel |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 7 "c1" (Ok { id = "c1", name = "Collection c1", version = 5, changed = True })) confirming
    in
    outs == [ Collections.BrowseEnded ]
        && Collections.browse settled == Nothing
        && List.map .id (Collections.items settled) == [ "c2" ]


caseDeleteFailurePreservesBrowsing : Bool
caseDeleteFailurePreservesBrowsing =
    let
        asked =
            run (Collections.DeleteRequested "c1") browsedModel |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 7 "c1" (Err (Http.BadStatus 500))) confirming
    in
    outs == []
        && Collections.browse settled == Just "c1"
        && List.length (Collections.items settled) == 2
        && Collections.status settled == Just "Search failed (HTTP 500)."


caseDeleteConflictPreservesBrowsing : Bool
caseDeleteConflictPreservesBrowsing =
    let
        asked =
            run (Collections.DeleteRequested "c1") browsedModel |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 7 "c1" (Err (Http.BadStatus 409))) confirming
    in
    outs == []
        && Collections.browse settled == Just "c1"
        && Collections.deleteConfirmId settled == Just "c1"
        && List.length (Collections.items settled) == 2


caseDeleteWrongResourceRejected : Bool
caseDeleteWrongResourceRejected =
    let
        asked =
            run (Collections.DeleteRequested "c1") withSummaries |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 7 "c1" (Ok { id = "c2", name = "Collection c2", version = 2, changed = True })) confirming
    in
    outs == []
        && Collections.status settled == Just "The delete response was invalid."
        && List.length (Collections.items settled) == 2


caseLockedDeleteBlocked : Bool
caseLockedDeleteBlocked =
    let
        asked =
            run (Collections.DeleteRequested "c1") withSummaries |> Tuple.first

        ( settled, outs ) =
            runLocked Collections.DeleteConfirmed asked
    in
    outs == []
        && Collections.status settled == Just "Unlock to delete collections."
        && List.length (Collections.items settled) == 2


caseLockClearsConfirmation : Bool
caseLockClearsConfirmation =
    let
        asked =
            run (Collections.DeleteRequested "c1") withSummaries |> Tuple.first

        cleared =
            Collections.locked { asked | status = Just "old", removals = Set.singleton "p1" }
    in
    Collections.deleteConfirmId cleared == Nothing
        && Collections.renamingId cleared == Nothing
        && Collections.browse cleared == Collections.browse asked


caseRealisticMoveAckRefreshesGrid : Bool
caseRealisticMoveAckRefreshesGrid =
    let
        summaries =
            run (Collections.SummariesCompleted 0 (Ok [ summary "7" 3, summary "8" 1 ])) Collections.init |> Tuple.first

        browsed =
            let
                opened =
                    run (Collections.OpenRequested "7") summaries |> Tuple.first
            in
            run (Collections.PostsCompleted (Collections.requestId opened) "7" (Ok (pageOf [ "42", "43" ] Nothing 3))) opened |> Tuple.first

        managedModel =
            let
                opened =
                    run (Collections.ManageRequested "7") browsed |> Tuple.first
            in
            run (Collections.ManageCompleted opened.manageRequestId "7" (Ok (pageOf [ "42", "43" ] Nothing 3))) opened |> Tuple.first

        moving =
            run (Collections.MoveRequested "42" 1) managedModel |> Tuple.first

        ( settled, _ ) =
            run (Collections.MoveCompleted 7 "7" "42" (Ok { id = "7", collectionId = "7", version = 4, changed = True })) moving
    in
    Collections.state settled == Collections.LoadingMore
        && Collections.browse settled == Just "7"
        && List.any (\c -> c.id == "7" && c.version == 4) (Collections.items settled)


caseStaleDeleteSuccessEndsBrowse : Bool
caseStaleDeleteSuccessEndsBrowse =
    let
        asked =
            run (Collections.DeleteRequested "c1") browsedModel |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 3 "c1" (Ok { id = "c1", name = "Collection c1", version = 5, changed = True })) confirming
    in
    outs == [ Collections.BrowseEnded ]
        && Collections.browse settled == Nothing
        && List.map .id (Collections.items settled) == [ "c2" ]


caseStaleDeleteAfterNavigationKeepsNewBrowse : Bool
caseStaleDeleteAfterNavigationKeepsNewBrowse =
    let
        asked =
            run (Collections.DeleteRequested "c1") browsedModel |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        navigated =
            run (Collections.OpenRequested "c2") confirming |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 3 "c1" (Ok { id = "c1", name = "Collection c1", version = 5, changed = True })) navigated
    in
    outs == []
        && Collections.browse settled == Just "c2"
        && List.map .id (Collections.items settled) == [ "c2" ]


caseStaleDelete401DoesNotRevoke : Bool
caseStaleDelete401DoesNotRevoke =
    let
        asked =
            run (Collections.DeleteRequested "c1") browsedModel |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        ( settled, outs ) =
            run (Collections.Deleted 3 "c1" (Err (Http.BadStatus 401))) confirming
    in
    outs == []
        && Collections.browse settled == Just "c1"
        && List.length (Collections.items settled) == 2


caseLockWhileDeleteThenStaleSuccess : Bool
caseLockWhileDeleteThenStaleSuccess =
    let
        asked =
            run (Collections.DeleteRequested "c1") browsedModel |> Tuple.first

        confirming =
            run Collections.DeleteConfirmed asked |> Tuple.first

        lockedModel =
            Collections.locked confirming

        ( settled, outs ) =
            run (Collections.Deleted 3 "c1" (Ok { id = "c1", name = "Collection c1", version = 5, changed = True })) lockedModel
    in
    outs == [ Collections.BrowseEnded ]
        && Collections.browse settled == Nothing
        && List.map .id (Collections.items settled) == [ "c2" ]


caseThousandMemberNavigation17Pages : Bool
caseThousandMemberNavigation17Pages =
    let
        feed n model =
            let
                fetching =
                    run Collections.ManageNextRequested model |> Tuple.first
            in
            run (Collections.ManageCompleted fetching.manageRequestId "c1" (Ok (pageOf (ids (n * 60 + 1) (if n == 16 then 40 else 60)) (if n == 16 then Nothing else Just "cur") 5))) fetching |> Tuple.first

        filled =
            List.foldl feed (managed (ids 1 60) (Just "cur")) (List.range 1 16)
    in
    (Collections.manageView filled |> Maybe.map (\v -> v.pageCount == 17 && List.length v.rows == 40 && not v.hasNext)) == Just True
        && Logic.maxManagePages == 1000


caseManageNextAtEndReports : Bool
caseManageNextAtEndReports =
    let
        model =
            managed [ "a", "b" ] Nothing

        ( settled, _ ) =
            run Collections.ManageNextRequested model
    in
    Collections.status settled == Just "Already at the last ordering page."


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "summary decodes with zero version", caseSummaryDecodes )
    , ( "summary missing field fails", caseSummaryMissingFieldFails )
    , ( "summary negative count fails", caseSummaryNegativeFails )
    , ( "legacy member payload is not a summary", caseLegacyPayloadIsNotSummary )
    , ( "summaries wrapper is required", caseSummariesWrapperRequired )
    , ( "move result decodes", caseMoveDecodes )
    , ( "move missing collection fails", caseMoveMissingCollectionFails )
    , ( "summary is not a move result", caseSummaryIsNotMove )
    , ( "manage result decodes", caseManageDecodes )
    , ( "move result is not a manage result", caseMoveIsNotManage )
    , ( "create ack ignores the member array", caseAckIgnoresMemberArray )
    , ( "plan up uses the predecessor", casePlanUpUsesPredecessor )
    , ( "plan down uses the following neighbor", casePlanDownUsesFollowingNeighbor )
    , ( "plan down can target the end", casePlanDownToEnd )
    , ( "plan down past the window needs a fetch", casePlanDownNeedsNextPage )
    , ( "plan edges are no-ops", casePlanEdgesAreNoops )
    , ( "plan rejects unknown posts and deltas", casePlanUnknownAndBadDelta )
    , ( "plan first targets the window head", casePlanFirst )
    , ( "plan last targets the end", casePlanLast )
    , ( "management page bounds", caseManagePageBounds )
    , ( "management renders one bounded page", caseManageRendersOnePage )
    , ( "management next fetches then advances", caseManageNextFetchesThenAdvances )
    , ( "management back returns a page", caseManageBackReturns )
    , ( "management fetching is bounded", caseManageFetchBound )
    , ( "cross-page up move fires", caseCrossPageUpMoveFires )
    , ( "first and last moves fire", caseFirstAndLastMovesFire )
    , ( "down at the window end fetches the neighbor", caseDownAtWindowEndFetchesNeighbor )
    , ( "pending down move resumes after fetch", casePendingDownMoveResumes )
    , ( "concurrent move is ignored", caseConcurrentMoveIgnored )
    , ( "move while ordering loads is ignored", caseMoveWhileLoadingIgnored )
    , ( "move success updates the version", caseMoveSuccessUpdatesVersion )
    , ( "move wrong-resource response rejected", caseMoveWrongResourceRejected )
    , ( "move conflict refreshes the browsed prefix", caseMoveConflictRefreshesBrowsed )
    , ( "navigation before move completion keeps the new browse", caseNavigationBeforeMoveCompletion )
    , ( "stale move completion reconciles only", caseStaleMoveCompletionReconcilesOnly )
    , ( "move 401 revokes access", caseMoveUnauthorizedRevokes )
    , ( "locked moves disabled but reads work", caseLockedMoveDisabledButReadsWork )
    , ( "summaries unavailable without fallback", caseSummariesUnavailable )
    , ( "summaries retry recovers", caseSummariesRetryRecovers )
    , ( "stale summaries are dropped", caseStaleSummariesDropped )
    , ( "rename with the same name is a no-op", caseRenameNoopSameName )
    , ( "rename missing state is a no-op", caseRenameMissingState )
    , ( "rename success updates the summary", caseRenameSuccess )
    , ( "rename fires with a zero version", caseRenameZeroVersionFires )
    , ( "rename wrong-resource response rejected", caseRenameWrongResourceRejected )
    , ( "rename conflict keeps browsing", caseRenameConflictKeepsBrowsing )
    , ( "locked rename is blocked", caseLockedRenameBlocked )
    , ( "delete needs explicit confirmation", caseDeleteNeedsConfirmation )
    , ( "delete missing state is a no-op", caseDeleteMissingState )
    , ( "delete success removes only the collection", caseDeleteSuccessAwayFromBrowse )
    , ( "deleting the browsed collection ends the browse", caseDeleteBrowsedCollectionEndsBrowse )
    , ( "delete failure preserves browsing", caseDeleteFailurePreservesBrowsing )
    , ( "delete conflict preserves browsing", caseDeleteConflictPreservesBrowsing )
    , ( "delete wrong-resource response rejected", caseDeleteWrongResourceRejected )
    , ( "locked delete is blocked", caseLockedDeleteBlocked )
    , ( "lock clears rename and delete intent", caseLockClearsConfirmation )
    , ( "realistic move ack refreshes grid", caseRealisticMoveAckRefreshesGrid )
    , ( "stale delete success ends browse", caseStaleDeleteSuccessEndsBrowse )
    , ( "stale delete after navigation keeps new browse", caseStaleDeleteAfterNavigationKeepsNewBrowse )
    , ( "stale delete 401 does not revoke", caseStaleDelete401DoesNotRevoke )
    , ( "lock while delete then stale success ends browse", caseLockWhileDeleteThenStaleSuccess )
    , ( "1000-member navigation spans 17 pages", caseThousandMemberNavigation17Pages )
    , ( "manage next at end reports limit", caseManageNextAtEndReports )
    ]
