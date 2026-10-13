module DailyUseTests exposing (allPassed, suite)

import Domain.Trash as TrashDomain
import Feature.Export as Export
import Feature.TagSuggestions as TagSuggestions
import Feature.Trash as Trash
import Http
import Json.Decode as Decode


ctx : Trash.Context
ctx =
    Trash.context "" (Just "tok") True 7


run : Trash.Msg -> Trash.Model -> ( Trash.Model, List Trash.OutMsg )
run msg model =
    let
        ( settled, _, outs ) =
            Trash.update ctx msg model
    in
    ( settled, outs )


trashPost : String -> TrashDomain.TrashPost
trashPost id =
    { id = id, mediaType = "image/jpeg", width = 10, height = 10, tags = [], deletedAt = "2026-01-01T00:00:00Z", moderationState = "published" }


caseTrashPageDecodesMetadataOnly : Bool
caseTrashPageDecodesMetadataOnly =
    Decode.decodeString TrashDomain.pageDecoder """{"posts":[{"id":"42","preview_url":"/p","original_url":"/o","media_type":"image/jpeg","width":10,"height":20,"tags":["a"],"deleted_at":"2026-01-01T00:00:00Z","moderation_state":"hidden"}],"next_cursor":null}"""
        == Ok { posts = [ { id = "42", mediaType = "image/jpeg", width = 10, height = 20, tags = [ "a" ], deletedAt = "2026-01-01T00:00:00Z", moderationState = "hidden" } ], nextCursor = Nothing }


caseTrashConfirmationNamesCount : Bool
caseTrashConfirmationNamesCount =
    let
        ( asked, _ ) =
            run (Trash.TrashSelectedRequested [ "1", "2", "3" ]) Trash.init
    in
    Trash.pendingTrashCount asked == Just 3


caseTrashLockClearsRows : Bool
caseTrashLockClearsRows =
    let
        opened =
            run Trash.OpenRequested Trash.init |> Tuple.first

        loaded =
            run (Trash.Loaded 7 opened.requestId (Ok { posts = [ trashPost "1" ], nextCursor = Nothing })) opened |> Tuple.first
    in
    Trash.items (Trash.locked loaded) == [] && not (Trash.isOpen (Trash.locked loaded))


caseTrashLatePrivilegedReadRejected : Bool
caseTrashLatePrivilegedReadRejected =
    let
        opened =
            run Trash.OpenRequested Trash.init |> Tuple.first

        ( settled, outs ) =
            run (Trash.Loaded 3 opened.requestId (Ok { posts = [ trashPost "1" ], nextCursor = Nothing })) opened
    in
    outs == [] && Trash.items settled == []


caseTrashRestoreRemovesRowAndReconciles : Bool
caseTrashRestoreRemovesRowAndReconciles =
    let
        opened =
            run Trash.OpenRequested Trash.init |> Tuple.first

        loaded =
            run (Trash.Loaded 7 opened.requestId (Ok { posts = [ trashPost "1" ], nextCursor = Nothing })) opened |> Tuple.first

        restoring =
            run (Trash.RestoreRequested "1") loaded |> Tuple.first

        ( settled, outs ) =
            run (Trash.Restored 7 [ "1" ] (Ok [ { id = "1", changed = True } ])) restoring
    in
    outs == [ Trash.RestoreApplied [ "1" ] ] && Trash.items settled == []


caseTrashSuccessNotRetriedAndFailureKeepsSelection : Bool
caseTrashSuccessNotRetriedAndFailureKeepsSelection =
    let
        asked =
            run (Trash.TrashSelectedRequested [ "1", "2" ]) Trash.init |> Tuple.first

        confirming =
            run Trash.TrashConfirmRequested asked |> Tuple.first

        ( failed, failedOuts ) =
            run (Trash.Trashed 7 [ "1", "2" ] (Err (Http.BadStatus 500))) confirming

        ( succeeded, successOuts ) =
            run (Trash.Trashed 7 [ "1", "2" ] (Ok [ { id = "1", changed = True }, { id = "2", changed = True } ])) confirming
    in
    failedOuts
        == []
        && Trash.pendingTrashCount failed
        == Just 2
        && successOuts
        == [ Trash.TrashApplied [ "1", "2" ] ]
        && Trash.pendingTrashCount succeeded
        == Nothing


caseTrashStale401DoesNotRevoke : Bool
caseTrashStale401DoesNotRevoke =
    let
        asked =
            run (Trash.TrashSelectedRequested [ "1" ]) Trash.init |> Tuple.first

        confirming =
            run Trash.TrashConfirmRequested asked |> Tuple.first

        ( _, outs ) =
            run (Trash.Trashed 3 [ "1" ] (Err (Http.BadStatus 401))) confirming
    in
    outs == []


caseExportSelectionUrlNoAuth : Bool
caseExportSelectionUrlNoAuth =
    Export.selectionUrl "" [ "1", "2" ] True
        == Just "/api/posts/export?ids=1%2C2&metadata=true"
        && Export.selectionUrl "https://x" [ "1" ] False
        == Just "https://x/api/posts/export?ids=1&metadata=false"
        && not (String.contains "token" (Export.selectionUrl "" [ "1" ] True |> Maybe.withDefault ""))


caseExportCollectionUrlAndGuards : Bool
caseExportCollectionUrlAndGuards =
    Export.collectionUrl "" "7" (Just 4) True
        == Just "/api/collections/7/export?expected_version=4&metadata=true"
        && Export.collectionUrl "" "7" Nothing True
        == Nothing
        && Export.selectionUrl "" [] True
        == Nothing
        && Export.validSelection (List.repeat 1001 "1")
        == False


caseTagSuggestionStaleDiscarded : Bool
caseTagSuggestionStaleDiscarded =
    let
        ( queried, _ ) =
            TagSuggestions.update (TagSuggestions.context "") (TagSuggestions.QueryChanged "alpha, be") TagSuggestions.init

        ( settled, _ ) =
            TagSuggestions.update (TagSuggestions.context "") (TagSuggestions.Completed (queried.requestId - 1) "be" (Ok [ { tag = "beta", count = 3 } ])) queried
    in
    TagSuggestions.suggestions settled == [] && TagSuggestions.shouldFetch "a" == False && TagSuggestions.shouldFetch "ab" == True


caseTagSuggestionAppliesToLastToken : Bool
caseTagSuggestionAppliesToLastToken =
    TagSuggestions.applySuggestion "alpha, be" "beta"
        == "alpha, beta, "
        && TagSuggestions.applySuggestion "be" "beta"
        == "beta, "
        && TagSuggestions.lastToken "alpha, be"
        == "be"


caseTagSuggestionDecoder : Bool
caseTagSuggestionDecoder =
    Decode.decodeString TagSuggestions.decoder """{"tags":[{"tag":"beta","count":3}]}""" == Ok [ { tag = "beta", count = 3 } ]


caseStaleTrashPreservesNewMutation : Bool
caseStaleTrashPreservesNewMutation =
    let
        current =
            run (Trash.TrashSelectedRequested [ "2" ]) Trash.init |> Tuple.first |> (run Trash.TrashConfirmRequested >> Tuple.first)

        ( late, outs ) =
            run (Trash.Trashed 3 [ "1" ] (Ok [ { id = "1", changed = True } ])) current

        ( failed, failedOuts ) =
            run (Trash.Trashed 3 [ "1" ] (Err (Http.BadStatus 500))) current

        ( restored, restoreOuts ) =
            run (Trash.Restored 3 [ "1" ] (Ok [ { id = "1", changed = True } ])) current
    in
    late == current && outs == [ Trash.TrashApplied [ "1" ] ] && failed == current && failedOuts == [] && restored == current && restoreOuts == [ Trash.RestoreApplied [ "1" ] ]


caseWrongTrashResourceRejected : Bool
caseWrongTrashResourceRejected =
    let
        current =
            run (Trash.TrashSelectedRequested [ "1" ]) Trash.init |> Tuple.first |> (run Trash.TrashConfirmRequested >> Tuple.first)

        ( settled, outs ) =
            run (Trash.Trashed 7 [ "1" ] (Ok [ { id = "99", changed = True } ])) current
    in
    outs == [] && Trash.pendingTrashCount settled == Just 1


caseTagSuggestionsClearAcrossNavigation : Bool
caseTagSuggestionsClearAcrossNavigation =
    let
        ( queried, _ ) =
            TagSuggestions.update (TagSuggestions.context "") (TagSuggestions.QueryChanged "ref") TagSuggestions.init

        ( ready, _ ) =
            TagSuggestions.update (TagSuggestions.context "") (TagSuggestions.Completed queried.requestId "ref" (Ok [ { tag = "reference", count = 1 } ])) queried

        cleared =
            TagSuggestions.clear ready

        ( newQuery, _ ) =
            TagSuggestions.update (TagSuggestions.context "") (TagSuggestions.QueryChanged "ref") cleared

        ( stale, _ ) =
            TagSuggestions.update (TagSuggestions.context "") (TagSuggestions.Completed queried.requestId "ref" (Ok [ { tag = "reference", count = 1 } ])) newQuery

        ( changed, _ ) =
            TagSuggestions.update (TagSuggestions.context "") (TagSuggestions.QueryChanged "oth") ready
    in
    TagSuggestions.suggestions cleared == [] && TagSuggestions.suggestions stale == [] && TagSuggestions.suggestions changed == [] && newQuery.requestId > queried.requestId


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "stale trash preserves newer mutation", caseStaleTrashPreservesNewMutation )
    , ( "wrong trash resource rejected", caseWrongTrashResourceRejected )
    , ( "tag suggestions cleared across navigation", caseTagSuggestionsClearAcrossNavigation )
    , ( "trash page decodes metadata only", caseTrashPageDecodesMetadataOnly )
    , ( "trash confirmation names count", caseTrashConfirmationNamesCount )
    , ( "trash lock clears rows", caseTrashLockClearsRows )
    , ( "trash late privileged read rejected", caseTrashLatePrivilegedReadRejected )
    , ( "trash restore removes row and reconciles", caseTrashRestoreRemovesRowAndReconciles )
    , ( "trash success not retried failure keeps selection", caseTrashSuccessNotRetriedAndFailureKeepsSelection )
    , ( "trash stale 401 does not revoke", caseTrashStale401DoesNotRevoke )
    , ( "export selection url has no auth", caseExportSelectionUrlNoAuth )
    , ( "export collection url and guards", caseExportCollectionUrlAndGuards )
    , ( "tag suggestions stale discarded", caseTagSuggestionStaleDiscarded )
    , ( "tag suggestion applies to last token", caseTagSuggestionAppliesToLastToken )
    , ( "tag suggestion decoder", caseTagSuggestionDecoder )
    ]
