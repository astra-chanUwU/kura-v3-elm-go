module RevisionTests exposing (allPassed, suite)

import Domain.Post exposing (TagRevision, revisionDecoder)
import Json.Decode as Decode


decode : String -> Result Decode.Error TagRevision
decode json =
    Decode.decodeString revisionDecoder json


case1 : Bool
case1 =
    case decode """{"version":1,"kind":"tag_edit","added_tags":["cat"],"removed_tags":[],"target_tags":null,"revertible":false,"created_at":"2026-01-01"}""" of
        Ok rev ->
            rev.targetTags == Nothing && rev.revertible == False

        Err _ ->
            False


case2 : Bool
case2 =
    case decode """{"version":2,"kind":"tag_edit","added_tags":[],"removed_tags":["cat"],"target_tags":[],"revertible":true,"created_at":"2026-01-02"}""" of
        Ok rev ->
            rev.targetTags == Just [] && rev.revertible == True

        Err _ ->
            False


case3 : Bool
case3 =
    case decode """{"version":3,"kind":"tag_edit","added_tags":[],"removed_tags":[],"created_at":"2026-01-03"}""" of
        Ok rev ->
            rev.targetTags == Nothing

        Err _ ->
            False


case4 : Bool
case4 =
    case decode """{"version":4,"kind":"tag_revert","added_tags":["cat"],"removed_tags":[],"target_tags":["cat","demo"],"revertible":true,"created_at":"2026-01-04"}""" of
        Ok rev ->
            rev.targetTags == Just [ "cat", "demo" ]

        Err _ ->
            False


case5 : Bool
case5 =
    case ( decode """{"version":5,"kind":"tag_edit","added_tags":[],"removed_tags":[],"target_tags":null,"created_at":"2026-01-05"}"""
         , decode """{"version":6,"kind":"tag_edit","added_tags":[],"removed_tags":[],"target_tags":[],"created_at":"2026-01-06"}"""
         ) of
        ( Ok revNull, Ok revEmpty ) ->
            revNull.revertible == False && revEmpty.revertible == True

        _ ->
            False


allPassed : Bool
allPassed =
    case1 && case2 && case3 && case4 && case5


suite : List ( String, Bool )
suite =
    [ ( "null target_tags decodes as Nothing and not revertible", case1 )
    , ( "empty array decodes as Just [] and revertible", case2 )
    , ( "missing target_tags decodes as Nothing legacy", case3 )
    , ( "populated target_tags decodes correctly", case4 )
    , ( "revertible computed from target_tags when field missing", case5 )
    ]
