module CollectionRefreshTests exposing (allPassed, suite)

import Domain.CollectionLogic as Logic
import Domain.Post exposing (PostSummary)
import Domain.Selection as Selection
import Domain.Sequence as Sequence
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


sel : Maybe String -> List String -> Selection.Selection
sel active selected =
    { active = active, selected = Set.fromList selected, anchor = active }


-- 1. shouldRefresh only when browsing same collection
caseShouldRefreshSame : Bool
caseShouldRefreshSame =
    Logic.shouldRefresh (Just "c1") "c1" == True


caseShouldRefreshOtherCollection : Bool
caseShouldRefreshOtherCollection =
    Logic.shouldRefresh (Just "c2") "c1" == False


caseShouldRefreshNotBrowsing : Bool
caseShouldRefreshNotBrowsing =
    Logic.shouldRefresh Nothing "c1" == False


-- 2. isStaleRequest guards obsolete reads
caseStaleRequestTrue : Bool
caseStaleRequestTrue =
    Logic.isStaleRequest 1 2 == True


caseStaleRequestFalse : Bool
caseStaleRequestFalse =
    Logic.isStaleRequest 5 5 == False


-- 3. isStaleGeneration guards old access token completion
caseStaleGenerationTrue : Bool
caseStaleGenerationTrue =
    Logic.isStaleGeneration 1 2 == True


caseStaleGenerationFalse : Bool
caseStaleGenerationFalse =
    Logic.isStaleGeneration 3 3 == False


-- 4. Preserve surviving selection and active post
casePreserveSurviving : Bool
casePreserveSurviving =
    let
        old =
            seq [ "1", "2", "3" ]

        new =
            seq [ "1", "2", "3" ]

        before =
            sel (Just "2") [ "1", "2" ]

        after =
            Logic.refreshSelection old new before
    in
    after.active == Just "2" && Set.member "1" after.selected && Set.member "2" after.selected


-- 5. Prune removed IDs
casePruneRemoved : Bool
casePruneRemoved =
    let
        old =
            seq [ "1", "2", "3" ]

        new =
            seq [ "1", "3" ]

        before =
            sel (Just "1") [ "1", "2", "3" ]

        after =
            Logic.refreshSelection old new before
    in
    not (Set.member "2" after.selected) && after.active == Just "1" && List.length (Set.toList after.selected) == 2


-- 6. Choose nearby active when active removed (middle)
caseNearbyMiddle : Bool
caseNearbyMiddle =
    let
        old =
            seq [ "1", "2", "3", "4" ]

        new =
            seq [ "1", "3", "4" ]

        before =
            sel (Just "2") [ "2" ]

        after =
            Logic.refreshSelection old new before
    in
    -- oldIdx=1, new at 1 is "3", so should pick "3"
    after.active == Just "3"


-- 7. Choose nearby when active was last removed
caseNearbyLast : Bool
caseNearbyLast =
    let
        old =
            seq [ "1", "2", "3" ]

        new =
            seq [ "1", "2" ]

        before =
            sel (Just "3") [ "3" ]

        after =
            Logic.refreshSelection old new before
    in
    -- oldIdx=2 out of bounds, fallback to oldIdx-1 =1 => "2"
    after.active == Just "2"


-- 8. Active removed, first element
caseNearbyFirst : Bool
caseNearbyFirst =
    let
        old =
            seq [ "1", "2", "3" ]

        new =
            seq [ "2", "3" ]

        before =
            sel (Just "1") [ "1" ]

        after =
            Logic.refreshSelection old new before
    in
    -- oldIdx 0, new at 0 is "2"
    after.active == Just "2"


-- 9. Empty new sequence clears active
caseEmptyClears : Bool
caseEmptyClears =
    let
        old =
            seq [ "1", "2" ]

        new =
            seq []

        before =
            sel (Just "1") [ "1", "2" ]

        after =
            Logic.refreshSelection old new before
    in
    after.active == Nothing && Set.isEmpty after.selected


-- 10. Non-active removal keeps active
caseNonActiveRemovalKeeps : Bool
caseNonActiveRemovalKeeps =
    let
        old =
            seq [ "1", "2", "3" ]

        new =
            seq [ "1", "2" ]

        before =
            sel (Just "1") [ "1", "3" ]

        after =
            Logic.refreshSelection old new before
    in
    after.active == Just "1" && not (Set.member "3" after.selected) && Set.member "1" after.selected


-- 11. Reorder preserves active and selected order not important but membership
caseReorderPreserves : Bool
caseReorderPreserves =
    let
        old =
            seq [ "1", "2", "3" ]

        new =
            seq [ "3", "1", "2" ]

        before =
            sel (Just "2") [ "1", "2" ]

        after =
            Logic.refreshSelection old new before
    in
    after.active == Just "2" && Set.member "1" after.selected


-- 12. Multiple selected plus active preserved
caseMultiplePreserve : Bool
caseMultiplePreserve =
    let
        old =
            seq [ "1", "2", "3", "4" ]

        new =
            seq [ "1", "3", "4" ]

        before =
            sel (Just "1") [ "1", "2", "4" ]

        after =
            Logic.refreshSelection old new before
    in
    after.active == Just "1" && Set.member "1" after.selected && not (Set.member "2" after.selected) && Set.member "4" after.selected


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "refresh same collection", caseShouldRefreshSame )
    , ( "refresh other collection ignored", caseShouldRefreshOtherCollection )
    , ( "refresh while not browsing ignored", caseShouldRefreshNotBrowsing )
    , ( "stale request true", caseStaleRequestTrue )
    , ( "stale request false", caseStaleRequestFalse )
    , ( "stale generation true", caseStaleGenerationTrue )
    , ( "stale generation false", caseStaleGenerationFalse )
    , ( "preserve surviving selection", casePreserveSurviving )
    , ( "prune removed ids", casePruneRemoved )
    , ( "nearby middle picks next", caseNearbyMiddle )
    , ( "nearby last picks previous", caseNearbyLast )
    , ( "nearby first picks next", caseNearbyFirst )
    , ( "empty clears active", caseEmptyClears )
    , ( "non-active removal keeps active", caseNonActiveRemovalKeeps )
    , ( "reorder preserves", caseReorderPreserves )
    , ( "multiple selected prune", caseMultiplePreserve )
    ]
