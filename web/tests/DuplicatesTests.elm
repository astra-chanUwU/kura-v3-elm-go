module DuplicatesTests exposing (allPassed, suite)

import Api.Duplicates as DupApi
import Domain.Duplicates as Dup
import Domain.Post as Post
import Domain.Rating as Rating
import Feature.Duplicates as DupF
import Http
import Json.Decode as Decode


ctx : DupF.Context
ctx =
    DupF.context "" Nothing True 7


lockedCtx : DupF.Context
lockedCtx =
    DupF.context "" Nothing False 7


hashA : String
hashA =
    "sha256:" ++ String.repeat 64 "a"


hashB : String
hashB =
    "sha256:" ++ String.repeat 64 "b"


hashC : String
hashC =
    "sha256:" ++ String.repeat 64 "c"


summary : String -> Post.PostSummary
summary id =
    { id = id, previewUrl = "/p" ++ id, originalUrl = "/o" ++ id, mediaType = "image/png", width = 10, height = 20, tags = [], rating = Rating.Unrated, ratingVersion = Nothing }


step : DupF.Msg -> DupF.Model -> ( DupF.Model, List DupF.OutMsg )
step msg model =
    let
        ( next, _, outs ) =
            DupF.update ctx msg model
    in
    ( next, outs )


stepWith : DupF.Context -> DupF.Msg -> DupF.Model -> ( DupF.Model, List DupF.OutMsg )
stepWith context msg model =
    let
        ( next, _, outs ) =
            DupF.update context msg model
    in
    ( next, outs )


opened : DupF.Model
opened =
    DupF.init |> step DupF.OpenRequested |> Tuple.first


groupsLoaded : DupF.Model
groupsLoaded =
    opened
        |> step (DupF.GroupsLoaded 7 (DupF.groupsRequestId opened) False (Ok { groups = [ { hash = hashA, count = 3, posts = [ summary "1" ] }, { hash = hashB, count = 2, posts = [] } ], nextCursor = Just "gc1" }))
        |> Tuple.first


inGroup : DupF.Model
inGroup =
    let
        opening =
            groupsLoaded |> step (DupF.GroupOpenRequested hashA) |> Tuple.first
    in
    opening
        |> step (DupF.MembersLoaded 7 (DupF.membersRequestId opening) hashA False (Ok { hash = hashA, count = 3, posts = [ summary "1", summary "2", summary "3" ], nextCursor = Just "mc1" }))
        |> Tuple.first


caseDecoderStrict : Bool
caseDecoderStrict =
    let
        good =
            """{"groups":[{"hash":"HASH_A","count":2,"posts":[]}],"next_cursor":null}""" |> String.replace "HASH_A" hashA

        badHash =
            """{"groups":[{"hash":"sha256:ABC","count":2,"posts":[]}],"next_cursor":null}"""

        upperHash =
            good |> String.replace hashA ("sha256:" ++ String.repeat 64 "A")

        lowCount =
            """{"groups":[{"hash":"HASH_A","count":1,"posts":[]}],"next_cursor":null}""" |> String.replace "HASH_A" hashA

        missingPosts =
            """{"groups":[{"hash":"HASH_A","count":2}],"next_cursor":null}""" |> String.replace "HASH_A" hashA
    in
    (Decode.decodeString Dup.groupsPageDecoder good |> Result.map (List.map .hash << .groups)) == Ok [ hashA ]
        && isErr (Decode.decodeString Dup.groupsPageDecoder badHash)
        && isErr (Decode.decodeString Dup.groupsPageDecoder upperHash)
        && isErr (Decode.decodeString Dup.groupsPageDecoder lowCount)
        && isErr (Decode.decodeString Dup.groupsPageDecoder missingPosts)
        && Dup.isCanonicalHash hashA
        && not (Dup.isCanonicalHash ("sha256:" ++ String.repeat 64 "A"))


isErr : Result e a -> Bool
isErr result =
    case result of
        Err _ ->
            True

        Ok _ ->
            False


caseMembersDecoder : Bool
caseMembersDecoder =
    let
        json =
            """{"hash":"HASH_A","count":2,"posts":[{"id":"1","preview_url":"/p","original_url":"/o","media_type":"image/png","width":1,"height":2}],"next_cursor":"x"}"""
                |> String.replace "HASH_A" hashA
    in
    (Decode.decodeString Dup.membersPageDecoder json |> Result.map (\page -> ( page.hash, page.count, ( List.map .id page.posts, page.nextCursor ) )))
        == Ok ( hashA, 2, ( [ "1" ], Just "x" ) )


caseEndpoints : Bool
caseEndpoints =
    DupApi.membersEndpoint "" hashA Nothing 60
        == ("/api/posts/duplicates/sha256%3A" ++ String.repeat 64 "a" ++ "?limit=60")
        && DupApi.membersEndpoint "https://x/" hashA (Just "a b") 60
        == ("https://x/api/posts/duplicates/sha256%3A" ++ String.repeat 64 "a" ++ "?limit=60&cursor=a%20b")
        && DupApi.groupsEndpoint "" Nothing 20
        == "/api/posts/duplicates?limit=20"


caseGroupsPaginationAppend : Bool
caseGroupsPaginationAppend =
    let
        loadingMore =
            groupsLoaded |> step DupF.GroupsLoadMoreRequested |> Tuple.first

        appended =
            loadingMore
                |> step (DupF.GroupsLoaded 7 (DupF.groupsRequestId loadingMore) True (Ok { groups = [ { hash = hashB, count = 9, posts = [] }, { hash = hashC, count = 2, posts = [] } ], nextCursor = Nothing }))
                |> Tuple.first
    in
    List.map .hash (DupF.groups appended) == [ hashA, hashB, hashC ]


caseGroupsStaleIgnored : Bool
caseGroupsStaleIgnored =
    let
        page =
            Ok { groups = [ { hash = hashC, count = 2, posts = [] } ], nextCursor = Nothing }

        wrongGen =
            groupsLoaded |> step (DupF.GroupsLoaded 3 (DupF.groupsRequestId groupsLoaded) False page) |> Tuple.first

        wrongId =
            groupsLoaded |> step (DupF.GroupsLoaded 7 (DupF.groupsRequestId groupsLoaded + 9) False page) |> Tuple.first

        closed =
            groupsLoaded |> step DupF.CloseRequested |> Tuple.first

        afterClose =
            closed |> step (DupF.GroupsLoaded 7 (DupF.groupsRequestId groupsLoaded) False page) |> Tuple.first
    in
    List.map .hash (DupF.groups wrongGen) == [ hashA, hashB ]
        && List.map .hash (DupF.groups wrongId) == [ hashA, hashB ]
        && List.map .hash (DupF.groups afterClose) == [ hashA, hashB ]


caseOverlappingLoadMoreIgnored : Bool
caseOverlappingLoadMoreIgnored =
    let
        again =
            opened |> step DupF.GroupsLoadMoreRequested |> Tuple.first
    in
    DupF.groupsRequestId again == DupF.groupsRequestId opened && DupF.groupsLoading opened


caseErrorPreservesGroups : Bool
caseErrorPreservesGroups =
    let
        reloading =
            groupsLoaded |> step DupF.GroupsReloadRequested |> Tuple.first

        failed =
            reloading |> step (DupF.GroupsLoaded 7 (DupF.groupsRequestId reloading) False (Err (Http.BadStatus 503))) |> Tuple.first
    in
    List.map .hash (DupF.groups failed) == [ hashA, hashB ] && DupF.status failed /= Nothing


caseMembersAppendAndPrune : Bool
caseMembersAppendAndPrune =
    let
        toggled =
            inGroup |> step (DupF.MemberToggled "9" True) |> Tuple.first

        loadingMore =
            inGroup |> step DupF.MembersLoadMoreRequested |> Tuple.first

        appended =
            loadingMore
                |> step (DupF.MembersLoaded 7 (DupF.membersRequestId loadingMore) hashA True (Ok { hash = hashA, count = 4, posts = [ summary "3", summary "4" ], nextCursor = Nothing }))
                |> Tuple.first
    in
    List.map .id (DupF.members appended) == [ "1", "2", "3", "4" ]
        && DupF.selectedIds toggled == []


caseMembersStaleOtherGroup : Bool
caseMembersStaleOtherGroup =
    let
        wrongHash =
            inGroup
                |> step (DupF.MembersLoaded 7 (DupF.membersRequestId inGroup) hashB False (Ok { hash = hashB, count = 2, posts = [ summary "8" ], nextCursor = Nothing }))
                |> Tuple.first

        openingB =
            inGroup |> step (DupF.GroupOpenRequested hashB) |> Tuple.first

        lateForA =
            openingB
                |> step (DupF.MembersLoaded 7 (DupF.membersRequestId inGroup) hashA False (Ok { hash = hashA, count = 3, posts = [ summary "1" ], nextCursor = Nothing }))
                |> Tuple.first
    in
    List.map .id (DupF.members wrongHash) == [ "1", "2", "3" ]
        && DupF.members lateForA == []
        && DupF.currentHash lateForA == Just hashB


caseSelectionIndependentMax : Bool
caseSelectionIndependentMax =
    let
        opening =
            groupsLoaded |> step (DupF.GroupOpenRequested hashA) |> Tuple.first

        many =
            List.map (summary << String.fromInt) (List.range 1 101)

        loadedMany =
            opening
                |> step (DupF.MembersLoaded 7 (DupF.membersRequestId opening) hashA False (Ok { hash = hashA, count = 101, posts = many, nextCursor = Nothing }))
                |> Tuple.first

        chosen =
            List.foldl (\id model -> step (DupF.MemberToggled id True) model |> Tuple.first) loadedMany (List.map (String.fromInt) (List.range 1 101))
    in
    List.length (DupF.selectedIds chosen) == 100 && DupF.status chosen /= Nothing


caseNoSelectionNoTrash : Bool
caseNoSelectionNoTrash =
    let
        ( _, outs ) =
            step DupF.TrashRequested inGroup
    in
    outs == [] && DupF.selectedIds inGroup == []


caseExplicitTrashAndLocked : Bool
caseExplicitTrashAndLocked =
    let
        chosen =
            inGroup |> step (DupF.MemberToggled "2" True) |> Tuple.first

        ( _, outs ) =
            step DupF.TrashRequested chosen

        ( lockedModel, lockedOuts ) =
            stepWith lockedCtx DupF.TrashRequested chosen
    in
    outs == [ DupF.TrashSelected [ "2" ] ]
        && lockedOuts == []
        && DupF.status lockedModel /= Nothing
        && DupF.selectedIds (DupF.locked chosen) == []


caseLockedReadsWork : Bool
caseLockedReadsWork =
    let
        openedLocked =
            DupF.init |> stepWith lockedCtx DupF.OpenRequested |> Tuple.first

        loadedLocked =
            openedLocked
                |> stepWith lockedCtx (DupF.GroupsLoaded 7 (DupF.groupsRequestId openedLocked) False (Ok { groups = [ { hash = hashA, count = 2, posts = [] } ], nextCursor = Nothing }))
                |> Tuple.first
    in
    List.map .hash (DupF.groups loadedLocked) == [ hashA ]


caseGroupChangePrunes : Bool
caseGroupChangePrunes =
    let
        chosen =
            inGroup |> step (DupF.MemberToggled "1" True) |> Tuple.first

        other =
            chosen |> step (DupF.GroupOpenRequested hashB) |> Tuple.first
    in
    DupF.selectedIds other == [] && DupF.members other == [] && DupF.currentHash other == Just hashB


caseComparePairs : Bool
caseComparePairs =
    let
        ( _, firstPairOuts ) =
            step DupF.CompareRequested inGroup

        chosen =
            inGroup
                |> step (DupF.MemberToggled "3" True)
                |> Tuple.first
                |> step (DupF.MemberToggled "2" True)
                |> Tuple.first

        ( _, chosenOuts ) =
            step DupF.CompareRequested chosen

        single =
            groupsLoaded |> step (DupF.GroupOpenRequested hashA) |> Tuple.first

        singleLoaded =
            single
                |> step (DupF.MembersLoaded 7 (DupF.membersRequestId single) hashA False (Ok { hash = hashA, count = 2, posts = [ summary "1" ], nextCursor = Nothing }))
                |> Tuple.first

        ( _, singleOuts ) =
            step DupF.CompareRequested singleLoaded
    in
    firstPairOuts == [ DupF.CompareCopies "1" "2" (DupF.members inGroup) ]
        && chosenOuts == [ DupF.CompareCopies "2" "3" (DupF.members inGroup) ]
        && singleOuts == []


caseMemberGroupGone404 : Bool
caseMemberGroupGone404 =
    let
        opening =
            groupsLoaded |> step (DupF.GroupOpenRequested hashA) |> Tuple.first

        gone =
            opening
                |> step (DupF.MembersLoaded 7 (DupF.membersRequestId opening) hashA False (Err (Http.BadStatus 404)))
                |> Tuple.first
    in
    DupF.currentHash gone == Nothing
        && DupF.members gone == []
        && DupF.groupsLoading gone
        && (DupF.status gone |> Maybe.map (String.contains "no longer available") |> Maybe.withDefault False)


caseReconcileTrash : Bool
caseReconcileTrash =
    let
        chosen =
            inGroup |> step (DupF.MemberToggled "1" True) |> Tuple.first

        ( afterTrash, _ ) =
            step (DupF.TrashApplied [ "1" ]) chosen

        pair =
            groupsLoaded |> step (DupF.GroupOpenRequested hashB) |> Tuple.first

        pairLoaded =
            pair
                |> step (DupF.MembersLoaded 7 (DupF.membersRequestId pair) hashB False (Ok { hash = hashB, count = 2, posts = [ summary "5", summary "6" ], nextCursor = Nothing }))
                |> Tuple.first

        ( gone, _ ) =
            step (DupF.TrashApplied [ "5" ]) pairLoaded
    in
    List.map .id (DupF.members afterTrash) == [ "2", "3" ]
        && DupF.selectedIds afterTrash == []
        && DupF.membersCount afterTrash == 2
        && DupF.currentHash gone == Nothing


caseReconcileRestoreReloads : Bool
caseReconcileRestoreReloads =
    let
        ( afterRestore, _ ) =
            step (DupF.RestoreApplied [ "9" ]) inGroup
    in
    DupF.groupsLoading afterRestore && DupF.membersLoading afterRestore


caseSelectionStillLoadedAtTrash : Bool
caseSelectionStillLoadedAtTrash =
    let
        chosen =
            inGroup |> step (DupF.MemberToggled "1" True) |> Tuple.first

        ( afterTrash, _ ) =
            step (DupF.TrashApplied [ "1" ]) chosen

        ( _, outs ) =
            step DupF.TrashRequested afterTrash
    in
    outs == []


caseUnrelatedTrashAndLatePage : Bool
caseUnrelatedTrashAndLatePage =
    let
        oldId =
            DupF.membersRequestId inGroup

        unrelated =
            inGroup |> step (DupF.TrashApplied [ "99" ]) |> Tuple.first

        afterLate =
            unrelated
                |> step (DupF.MembersLoaded 7 oldId hashA False (Ok { hash = hashA, count = 9, posts = [ summary "99" ], nextCursor = Nothing }))
                |> Tuple.first
    in
    DupF.currentHash unrelated == Just hashA
        && DupF.membersCount unrelated == 3
        && DupF.membersLoading unrelated
        && List.map .id (DupF.members afterLate) == [ "1", "2", "3" ]
        && DupF.membersCount afterLate == 3


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "duplicates unrelated Trash preserves count and invalidates late page", caseUnrelatedTrashAndLatePage )
    , ( "duplicates decoder strict canonical hash", caseDecoderStrict )
    , ( "duplicates members decoder", caseMembersDecoder )
    , ( "duplicates endpoints encode hash and cursor", caseEndpoints )
    , ( "duplicates groups pagination append dedupes", caseGroupsPaginationAppend )
    , ( "duplicates stale group responses ignored", caseGroupsStaleIgnored )
    , ( "duplicates overlapping load more ignored", caseOverlappingLoadMoreIgnored )
    , ( "duplicates error preserves groups", caseErrorPreservesGroups )
    , ( "duplicates members append and unknown toggle pruned", caseMembersAppendAndPrune )
    , ( "duplicates stale member responses for other group ignored", caseMembersStaleOtherGroup )
    , ( "duplicates selection max 100", caseSelectionIndependentMax )
    , ( "duplicates no selection does not trash", caseNoSelectionNoTrash )
    , ( "duplicates explicit trash and locked gate", caseExplicitTrashAndLocked )
    , ( "duplicates locked reads work", caseLockedReadsWork )
    , ( "duplicates group change prunes selection", caseGroupChangePrunes )
    , ( "duplicates compare first pair and chosen pair", caseComparePairs )
    , ( "duplicates member 404 explains and reloads groups", caseMemberGroupGone404 )
    , ( "duplicates reconcile after trash", caseReconcileTrash )
    , ( "duplicates reconcile after restore reloads bounded", caseReconcileRestoreReloads )
    , ( "duplicates trashed selection cannot trash again", caseSelectionStillLoadedAtTrash )
    ]
