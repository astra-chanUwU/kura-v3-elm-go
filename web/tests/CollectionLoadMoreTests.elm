module CollectionLoadMoreTests exposing (allPassed, suite)

import Domain.CollectionLogic as Logic
import Domain.Post exposing (PostSummary)
import Domain.Selection as Selection
import Domain.Sequence as Sequence
import Http
import Set


summary : String -> PostSummary
summary id =
    { id = id
    , previewUrl = "/media/" ++ id
    , originalUrl = "/media/o" ++ id
    , mediaType = "image/jpeg"
    , width = 100
    , height = 100
    , tags = []
    }


seq : List String -> Sequence.Sequence
seq ids =
    Sequence.fromList (List.map summary ids)


pageIds : Int -> Int -> List String
pageIds start count =
    List.range start (start + count - 1) |> List.map (\n -> "p" ++ String.fromInt n)


appendPages : List (List String) -> Sequence.Sequence
appendPages pages =
    List.foldl (\ids acc -> Sequence.append (List.map summary ids) acc) Sequence.empty pages



-- Failure classification: 409 vs 400 vs transport


caseChanged409 : Bool
caseChanged409 =
    Logic.classifyCollectionError (Http.BadStatus 409) == Logic.CollectionChanged


caseInvalidCursor400 : Bool
caseInvalidCursor400 =
    Logic.classifyCollectionError (Http.BadStatus 400) == Logic.InvalidCursor


caseTransport404 : Bool
caseTransport404 =
    Logic.classifyCollectionError (Http.BadStatus 404) == Logic.TransportError


caseTransport500 : Bool
caseTransport500 =
    Logic.classifyCollectionError (Http.BadStatus 500) == Logic.TransportError


caseTransportTimeout : Bool
caseTransportTimeout =
    Logic.classifyCollectionError Http.Timeout == Logic.TransportError


caseTransportNetwork : Bool
caseTransportNetwork =
    Logic.classifyCollectionError Http.NetworkError == Logic.TransportError


caseTransportBadBody : Bool
caseTransportBadBody =
    Logic.classifyCollectionError (Http.BadBody "x") == Logic.TransportError


caseTransportBadUrl : Bool
caseTransportBadUrl =
    Logic.classifyCollectionError (Http.BadUrl "x") == Logic.TransportError



-- Refresh targets and page bounds


caseTargetEmpty : Bool
caseTargetEmpty =
    Logic.refreshTarget 0 == 60


caseTargetPartial : Bool
caseTargetPartial =
    Logic.refreshTarget 30 == 60


caseTargetFullPage : Bool
caseTargetFullPage =
    Logic.refreshTarget 60 == 60


caseTargetMultiPage : Bool
caseTargetMultiPage =
    Logic.refreshTarget 150 == 150


caseTargetLarge : Bool
caseTargetLarge =
    Logic.refreshTarget 1000 == 1000


casePagesEmpty : Bool
casePagesEmpty =
    Logic.refreshPageCount 0 == 1


casePagesSingle : Bool
casePagesSingle =
    Logic.refreshPageCount 60 == 1


casePagesTwo : Bool
casePagesTwo =
    Logic.refreshPageCount 61 == 2 && Logic.refreshPageCount 120 == 2


casePages150 : Bool
casePages150 =
    -- 60/60/30 traversal takes three bounded pages.
    Logic.refreshPageCount 150 == 3


casePages1000 : Bool
casePages1000 =
    Logic.refreshPageCount 1000 == 17


casePagesBounded : Bool
casePagesBounded =
    Logic.refreshPageCount 100000 == Logic.maxRefreshPages



-- Bounded recovery retries


caseRetryFresh : Bool
caseRetryFresh =
    Logic.shouldRetryRefresh 0 == True


caseRetryMid : Bool
caseRetryMid =
    Logic.shouldRetryRefresh 2 == True


caseRetryExhausted : Bool
caseRetryExhausted =
    Logic.shouldRetryRefresh 3 == False && Logic.shouldRetryRefresh 9 == False



-- Refresh completion: final page stops even below target


caseNeedsMore : Bool
caseNeedsMore =
    Logic.needsMoreRefreshPages 120 150 (Just "cursor") == True


caseNeedsMoreAtTarget : Bool
caseNeedsMoreAtTarget =
    Logic.needsMoreRefreshPages 150 150 (Just "cursor") == False


caseFinalPageStops : Bool
caseFinalPageStops =
    -- The collection shrank below the target: no cursor, no more pages.
    Logic.needsMoreRefreshPages 30 60 Nothing == False


caseEmptyFinalStops : Bool
caseEmptyFinalStops =
    Logic.needsMoreRefreshPages 0 60 Nothing == False



-- Response guards: stale request ids and navigation


caseCurrentResponse : Bool
caseCurrentResponse =
    Logic.isObsoleteResponse 7 7 (Just "c1") "c1" == False


caseStaleRequestObsolete : Bool
caseStaleRequestObsolete =
    -- A mutation or newer load bumped the request id: late append is dropped.
    Logic.isObsoleteResponse 7 8 (Just "c1") "c1" == True


caseOtherCollectionObsolete : Bool
caseOtherCollectionObsolete =
    Logic.isObsoleteResponse 7 7 (Just "c2") "c1" == True


caseNavigatedAwayObsolete : Bool
caseNavigatedAwayObsolete =
    -- A query change cleared the browsed collection: response is dropped.
    Logic.isObsoleteResponse 7 7 Nothing "c1" == True



-- Traversal: 150 members in 60/60/30 pages, no gaps or duplicates


caseTraversal150 : Bool
caseTraversal150 =
    let
        assembled =
            appendPages [ pageIds 1 60, pageIds 61 60, pageIds 121 30 ]
    in
    Sequence.length assembled == 150 && Sequence.ids assembled == pageIds 1 150


caseTraversal1000 : Bool
caseTraversal1000 =
    let
        pages =
            List.range 0 15
                |> List.map (\i -> pageIds (i * 60 + 1) 60)
                |> (\full -> full ++ [ pageIds 961 40 ])

        assembled =
            appendPages pages
    in
    Sequence.length assembled == 1000 && Sequence.ids assembled == pageIds 1 1000


caseDuplicatePagesIgnored : Bool
caseDuplicatePagesIgnored =
    let
        once =
            appendPages [ pageIds 1 60, pageIds 61 60 ]

        twice =
            appendPages [ pageIds 1 60, pageIds 61 60, pageIds 1 60, pageIds 61 30 ]
    in
    Sequence.length once == 120 && Sequence.length twice == 120 && Sequence.ids twice == pageIds 1 120


caseOverlappingPagesDeduped : Bool
caseOverlappingPagesDeduped =
    let
        assembled =
            appendPages [ pageIds 1 60, pageIds 51 60 ]
    in
    Sequence.length assembled == 110 && Sequence.ids assembled == pageIds 1 110



-- Appending preserves the workspace selection


caseAppendKeepsSelection : Bool
caseAppendKeepsSelection =
    let
        before =
            seq (pageIds 1 60)

        after =
            Sequence.append (List.map summary (pageIds 61 60)) before

        selection =
            { active = Just "p7", selected = Set.fromList [ "p7", "p9" ], anchor = Just "p7" }

        pruned =
            Selection.prune after selection
    in
    pruned.active == Just "p7" && pruned.selected == selection.selected && Sequence.length after == 120


caseAppendKeepsActiveAtEnd : Bool
caseAppendKeepsActiveAtEnd =
    let
        before =
            seq (pageIds 1 60)

        after =
            Sequence.append (List.map summary (pageIds 61 30)) before

        pruned =
            Selection.prune after { active = Just "p60", selected = Set.fromList [ "p60" ], anchor = Just "p60" }
    in
    pruned.active == Just "p60" && Sequence.indexOf "p60" after == Just 59



-- Stale-epoch mutation recovery: Lock before completion, navigation


caseStaleBrowsedRefreshes : Bool
caseStaleBrowsedRefreshes =
    -- Lock landed before the mutation completed, but the affected
    -- collection is still browsed: rebuild its loaded prefix.
    Logic.staleMutationRecovery (Just "c1") "c1" == Logic.RefreshBrowsedCollection


caseStaleOtherCollectionSkipsRefresh : Bool
caseStaleOtherCollectionSkipsRefresh =
    -- Navigated to another collection before completion: only public
    -- metadata re-reads, the other workspace is left alone.
    Logic.staleMutationRecovery (Just "c2") "c1" == Logic.RefreshMetadataOnly


caseStaleAfterNavigatingHomeSkipsRefresh : Bool
caseStaleAfterNavigatingHomeSkipsRefresh =
    -- Back to global search before completion: no collection refresh.
    Logic.staleMutationRecovery Nothing "c1" == Logic.RefreshMetadataOnly



-- Scroll bounds against rebuilt content


caseScrollPreservedWhenValid : Bool
caseScrollPreservedWhenValid =
    Logic.clampScrollTop 2000 800 500 == 500


caseScrollClampedAfterShrink : Bool
caseScrollClampedAfterShrink =
    -- The refreshed collection shrank: the old offset clamps to the
    -- new maximum instead of stranding the grid below content.
    Logic.clampScrollTop 1000 800 500 == 200


caseScrollEmptyCollection : Bool
caseScrollEmptyCollection =
    Logic.clampScrollTop 0 800 500 == 0


caseScrollShortContent : Bool
caseScrollShortContent =
    Logic.clampScrollTop 400 800 0 == 0


caseScrollAtExactMax : Bool
caseScrollAtExactMax =
    Logic.clampScrollTop 2000 800 1200 == 1200


caseScrollNegativeClampsToZero : Bool
caseScrollNegativeClampsToZero =
    Logic.clampScrollTop 2000 800 -50 == 0


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "409 classifies as collection_changed", caseChanged409 )
    , ( "400 classifies as invalid cursor", caseInvalidCursor400 )
    , ( "404 is a transport error", caseTransport404 )
    , ( "500 is a transport error", caseTransport500 )
    , ( "timeout is a transport error", caseTransportTimeout )
    , ( "network failure is a transport error", caseTransportNetwork )
    , ( "bad body is a transport error", caseTransportBadBody )
    , ( "bad url is a transport error", caseTransportBadUrl )
    , ( "empty refresh targets one page", caseTargetEmpty )
    , ( "partial page refresh targets one page", caseTargetPartial )
    , ( "full page refresh targets itself", caseTargetFullPage )
    , ( "multi-page refresh targets prefix", caseTargetMultiPage )
    , ( "large refresh targets prefix", caseTargetLarge )
    , ( "empty needs one page", casePagesEmpty )
    , ( "single page needs one page", casePagesSingle )
    , ( "61-120 members need two pages", casePagesTwo )
    , ( "150 members need three pages", casePages150 )
    , ( "1000 members need seventeen pages", casePages1000 )
    , ( "refresh pages are bounded", casePagesBounded )
    , ( "fresh refresh may retry", caseRetryFresh )
    , ( "mid refresh may retry", caseRetryMid )
    , ( "exhausted refresh stops", caseRetryExhausted )
    , ( "refresh continues with cursor", caseNeedsMore )
    , ( "refresh stops at target", caseNeedsMoreAtTarget )
    , ( "final page stops refresh", caseFinalPageStops )
    , ( "empty final page stops refresh", caseEmptyFinalStops )
    , ( "current response accepted", caseCurrentResponse )
    , ( "stale request dropped", caseStaleRequestObsolete )
    , ( "other collection dropped", caseOtherCollectionObsolete )
    , ( "navigated-away response dropped", caseNavigatedAwayObsolete )
    , ( "150-member traversal has no gaps", caseTraversal150 )
    , ( "1000-member traversal has no gaps", caseTraversal1000 )
    , ( "duplicate pages ignored", caseDuplicatePagesIgnored )
    , ( "overlapping pages deduped", caseOverlappingPagesDeduped )
    , ( "append keeps selection", caseAppendKeepsSelection )
    , ( "append keeps end active", caseAppendKeepsActiveAtEnd )
    , ( "stale browsed mutation refreshes", caseStaleBrowsedRefreshes )
    , ( "stale other-collection skips refresh", caseStaleOtherCollectionSkipsRefresh )
    , ( "stale after navigating home skips refresh", caseStaleAfterNavigatingHomeSkipsRefresh )
    , ( "valid scroll preserved", caseScrollPreservedWhenValid )
    , ( "scroll clamps after shrink", caseScrollClampedAfterShrink )
    , ( "empty collection scrolls to zero", caseScrollEmptyCollection )
    , ( "short content scrolls to zero", caseScrollShortContent )
    , ( "scroll at exact max kept", caseScrollAtExactMax )
    , ( "negative scroll clamps to zero", caseScrollNegativeClampsToZero )
    ]
