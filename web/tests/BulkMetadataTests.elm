module BulkMetadataTests exposing (allPassed, suite)

import Api.Metadata as MetadataApi
import Domain.Post as Post
import Domain.Rating
import Feature.BulkMetadata as Bulk
import Http
import Json.Decode as Decode
import Json.Encode as Encode


ctx : Bulk.Context
ctx =
    Bulk.context "" (Just "tok") True 7


snap : String -> Int -> String -> String -> Post.MetadataSnapshot
snap id version source artist =
    { id = id, version = version, source = source, artist = artist }


step : Bulk.Msg -> Bulk.Model -> ( Bulk.Model, List Bulk.OutMsg )
step msg model =
    let
        ( next, _, outs ) =
            Bulk.update ctx msg model
    in
    ( next, outs )


loaded : Bulk.Model
loaded =
    Bulk.init
        |> step (Bulk.SelectionSync [ "1", "2" ])
        |> Tuple.first
        |> step (Bulk.ReadCompleted 7 2 [ "1", "2" ] (Ok { posts = [ snap "1" 0 "s1" "a1", snap "2" 3 "s2" "a1" ] }))
        |> Tuple.first


drafted : Bulk.Model
drafted =
    loaded
        |> step (Bulk.SourceCheckChanged True)
        |> Tuple.first
        |> step (Bulk.SourceChanged "new")
        |> Tuple.first


reviewed : Bulk.Model
reviewed =
    drafted |> step Bulk.ReviewRequested |> Tuple.first


saving : Bulk.Model
saving =
    reviewed |> step Bulk.SaveRequested |> Tuple.first


okResp : Post.MetadataResponse
okResp =
    { posts =
        [ { id = "1", version = 1, source = "new", artist = "a1", changed = True }
        , { id = "2", version = 4, source = "new", artist = "a1", changed = True }
        ]
    }


caseReadDecodeStrict : Bool
caseReadDecodeStrict =
    let
        good =
            """{"posts":[{"id":"1","version":0,"source":"","artist":"a"}]}"""

        missing =
            """{"posts":[{"id":"1","source":"","artist":"a"}]}"""

        nullVersion =
            """{"posts":[{"id":"1","version":null,"source":"","artist":"a"}]}"""
    in
    (Decode.decodeString Post.metadataReadResponseDecoder good |> Result.map (List.map .version << .posts)) == Ok [ 0 ]
        && (case Decode.decodeString Post.metadataReadResponseDecoder missing of
                Err _ ->
                    True

                Ok _ ->
                    False
           )
        && (case Decode.decodeString Post.metadataReadResponseDecoder nullVersion of
                Err _ ->
                    True

                Ok _ ->
                    False
           )


caseEncodeRead : Bool
caseEncodeRead =
    Encode.encode 0 (MetadataApi.encodeReadRequest [ "1", "2" ]) == """{"post_ids":["1","2"]}"""
        && MetadataApi.readEndpoint "" == "/api/posts/metadata/read"


caseMixedAndUncheckedOmission : Bool
caseMixedAndUncheckedOmission =
    Bulk.commonSource loaded == Nothing
        && Bulk.commonArtist loaded == Just "a1"
        && Bulk.desiredSource loaded == Nothing
        && Bulk.desiredArtist loaded == Nothing
        && (drafted |> Bulk.desiredSource) == Just "new"
        && (drafted |> Bulk.desiredArtist) == Nothing


caseIntentionalClear : Bool
caseIntentionalClear =
    let
        model =
            loaded
                |> step (Bulk.ArtistCheckChanged True)
                |> Tuple.first
                |> step (Bulk.ArtistChanged "")
                |> Tuple.first
    in
    Bulk.desiredArtist model == Just "" && Bulk.reviewing (model |> step Bulk.ReviewRequested |> Tuple.first)


caseCaptureVersionsZero : Bool
caseCaptureVersionsZero =
    Bulk.baseVersion "1" loaded == Just 0
        && Bulk.baseVersion "2" loaded == Just 3
        && Bulk.capturedTargets saving == [ { id = "1", version = 0 }, { id = "2", version = 3 } ]


caseOver100Disabled : Bool
caseOver100Disabled =
    let
        ids =
            List.range 1 101 |> List.map String.fromInt

        model =
            Bulk.init |> step (Bulk.SelectionSync ids) |> Tuple.first

        ( reviewedModel, _ ) =
            step Bulk.ReviewRequested model
    in
    Bulk.overLimit model
        && not (Bulk.loaded model)
        && not (Bulk.reviewing reviewedModel)
        && (Bulk.status reviewedModel |> Maybe.map (String.contains "100") |> Maybe.withDefault False)


caseStaleGuards : Bool
caseStaleGuards =
    let
        syncing =
            Bulk.init |> step (Bulk.SelectionSync [ "1", "2" ]) |> Tuple.first

        ( staleGen, outsGen ) =
            step (Bulk.ReadCompleted 3 (Bulk.readRequestId syncing) [ "1", "2" ] (Ok { posts = [ snap "1" 0 "s" "a", snap "2" 0 "s" "a" ] })) syncing

        ( staleId, outsId ) =
            step (Bulk.ReadCompleted 7 999 [ "1", "2" ] (Ok { posts = [ snap "1" 0 "s" "a", snap "2" 0 "s" "a" ] })) syncing

        ( staleWriteId, outsWrite ) =
            step (Bulk.WriteCompleted 7 999 [ { id = "1", version = 0 } ] (Just "x") Nothing [ snap "1" 0 "s" "a" ] (Ok { posts = [ { id = "1", version = 1, source = "x", artist = "a", changed = True } ] })) saving
    in
    outsGen == [] && not (Bulk.loaded staleGen)
        && outsId == [] && not (Bulk.loaded staleId)
        && outsWrite == [] && Bulk.saving staleWriteId


caseFieldChangeInvalidatesReview : Bool
caseFieldChangeInvalidatesReview =
    Bulk.reviewing reviewed
        && not (Bulk.reviewing (reviewed |> step (Bulk.SourceChanged "other") |> Tuple.first))
        && not (Bulk.reviewing (reviewed |> step (Bulk.ArtistCheckChanged True) |> Tuple.first))
        && not (Bulk.reviewing (reviewed |> step (Bulk.SelectionSync [ "1", "3" ]) |> Tuple.first))


caseConflictPreservedReload : Bool
caseConflictPreservedReload =
    let
        ( conflicted, _ ) =
            step (Bulk.WriteCompleted 7 (Bulk.requestId saving) [ { id = "1", version = 0 }, { id = "2", version = 3 } ] (Just "new") Nothing (Bulk.captured saving) (Err (Http.BadStatus 409))) saving

        ( blocked, _ ) =
            step Bulk.ReviewRequested conflicted

        reloading =
            conflicted |> step Bulk.ReloadRequested |> Tuple.first

        reloaded =
            reloading
                |> step (Bulk.ReadCompleted 7 (Bulk.readRequestId reloading) [ "1", "2" ] (Ok { posts = [ snap "1" 5 "ext" "a1", snap "2" 6 "s2" "a1" ] }))
                |> Tuple.first
    in
    Bulk.conflict conflicted
        && Bulk.sourceDraft conflicted == "new"
        && Bulk.changeSource conflicted
        && not (Bulk.reviewing blocked)
        && Bulk.baseVersion "1" reloaded == Just 5
        && Bulk.sourceDraft reloaded == "new"
        && not (Bulk.conflict reloaded)
        && not (Bulk.needsReload reloaded)
        && Bulk.reviewing (reloaded |> step Bulk.ReviewRequested |> Tuple.first)


caseDuplicateSubmit : Bool
caseDuplicateSubmit =
    let
        ( again, _ ) =
            step Bulk.SaveRequested saving
    in
    Bulk.saving again && Bulk.requestId again == Bulk.requestId saving && Bulk.capturedTargets again == Bulk.capturedTargets saving


caseNavMidWrite : Bool
caseNavMidWrite =
    let
        moved =
            saving
                |> step (Bulk.SelectionSync [ "3", "4" ])
                |> Tuple.first
                |> step (Bulk.ReadCompleted 7 3 [ "3", "4" ] (Ok { posts = [ snap "3" 0 "s3" "a3", snap "4" 0 "s4" "a4" ] }))
                |> Tuple.first

        draftedNew =
            moved
                |> step (Bulk.ArtistCheckChanged True)
                |> Tuple.first
                |> step (Bulk.ArtistChanged "other")
                |> Tuple.first

        ( settled, outs ) =
            step (Bulk.WriteCompleted 7 (Bulk.requestId saving) [ { id = "1", version = 0 }, { id = "2", version = 3 } ] (Just "new") Nothing (Bulk.captured saving) (Ok okResp)) draftedNew
    in
    (outs |> List.length) == 1
        && Bulk.ownerIds settled == [ "3", "4" ]
        && Bulk.artistDraft settled == "other"
        && (Bulk.knownSnapshot "1" settled |> Maybe.map .version) == Just 1
        && not (Bulk.saving settled)


caseMalformedReceiptsUncertain : Bool
caseMalformedReceiptsUncertain =
    List.all
        (\resp ->
            let
                ( settled, outs ) =
                    step (Bulk.WriteCompleted 7 (Bulk.requestId saving) [ { id = "1", version = 0 }, { id = "2", version = 3 } ] (Just "new") Nothing (Bulk.captured saving) (Ok resp)) saving
            in
            outs == [ Bulk.Uncertain [ "1", "2" ] ] && Bulk.needsReload settled && not (Bulk.saving settled)
        )
        [ { posts = [ { id = "1", version = 1, source = "new", artist = "a1", changed = True } ] }
        , { posts = [ { id = "1", version = 1, source = "new", artist = "WRONG", changed = True }, { id = "2", version = 4, source = "new", artist = "a1", changed = True } ] }
        , { posts = [ { id = "1", version = 2, source = "new", artist = "a1", changed = True }, { id = "2", version = 4, source = "new", artist = "a1", changed = True } ] }
        , { posts = [ { id = "1", version = 1, source = "new", artist = "a1", changed = True }, { id = "1", version = 1, source = "new", artist = "a1", changed = True } ] }
        , { posts = [ { id = "01", version = 1, source = "new", artist = "a1", changed = True }, { id = "2", version = 4, source = "new", artist = "a1", changed = True } ] }
        ]
        && List.all
            (\err ->
                let
                    ( settled, outs ) =
                        step (Bulk.WriteCompleted 7 (Bulk.requestId saving) [ { id = "1", version = 0 }, { id = "2", version = 3 } ] (Just "new") Nothing (Bulk.captured saving) (Err err)) saving
                in
                outs == [ Bulk.Uncertain [ "1", "2" ] ] && Bulk.needsReload settled
            )
            [ Http.Timeout, Http.NetworkError, Http.BadBody "bad", Http.BadStatus 503 ]


caseNoopVersions : Bool
caseNoopVersions =
    let
        model =
            loaded
                |> step (Bulk.SourceCheckChanged True)
                |> Tuple.first
                |> step (Bulk.SourceChanged "s1")
                |> Tuple.first
                |> step (Bulk.ArtistCheckChanged True)
                |> Tuple.first
                |> step (Bulk.ArtistChanged "a1")
                |> Tuple.first

        targets =
            [ { id = "1", version = 0 }, { id = "2", version = 3 } ]

        originals =
            Bulk.captured loaded

        -- Row 1 is a no-op for source (already s1) but row 2 changes; artist is a no-op for both.
        -- Receipt: row1 changed only if source differs (it does not) -> actually row1 no change at all.
        resp =
            { posts =
                [ { id = "1", version = 0, source = "s1", artist = "a1", changed = False }
                , { id = "2", version = 4, source = "s1", artist = "a1", changed = True }
                ]
            }
    in
    Bulk.validateReceipt targets (Just "s1") (Just "a1") originals resp
        && not (Bulk.validateReceipt targets (Just "s1") (Just "a1") originals { posts = [ { id = "1", version = 1, source = "s1", artist = "a1", changed = False }, { id = "2", version = 4, source = "s1", artist = "a1", changed = True } ] })


caseMonotonicPairs : Bool
caseMonotonicPairs =
    let
        noted =
            Bulk.noteSnapshot (snap "1" 9 "newer" "artist9") loaded

        older =
            Bulk.noteSnapshot (snap "1" 1 "old" "oldartist") noted

        oldDetail =
                { id = "1"
                , previewUrl = "/p"
                , originalUrl = "/o"
                , mediaType = "image/jpeg"
                , width = 1
                , height = 1
                , source = "old"
                , artist = "oldartist"
                , hash = "h"
                , fileSize = 1
                , createdAt = "2026-01-01"
                , tags = []
                , tagVersion = 0
                , favorite = False
                , score = 0
                , reactionVersion = 0
                , history = []
                , reactionHistory = []
                , derivativeJobId = Nothing
                , derivativeStatus = Nothing
                , metadataVersion = 2
                , metadataHistory = []
                , rating = Domain.Rating.Unrated
                , ratingVersion = Just 0
                , ratingHistory = []
                }

        detailOlder =
            Bulk.noteDetail oldDetail noted

        protected =
            Bulk.protectDetail noted oldDetail
    in
    (Bulk.knownSnapshot "1" older |> Maybe.map (\s -> ( s.version, s.source, s.artist ))) == Just ( 9, "newer", "artist9" )
        && (Bulk.knownSnapshot "1" detailOlder |> Maybe.map (\s -> ( s.version, s.source ))) == Just ( 9, "newer" )
        && ( protected.metadataVersion, protected.source, protected.artist ) == ( 9, "newer", "artist9" )
        && protected.metadataHistory == []


caseDirtyBaseFrozen : Bool
caseDirtyBaseFrozen =
    let
        noted =
            Bulk.noteSnapshot (snap "1" 8 "server" "aX") drafted
    in
    Bulk.baseVersion "1" noted == Just 0
        && Bulk.sourceDraft noted == "new"
        && (Bulk.knownSnapshot "1" noted |> Maybe.map .version) == Just 8


caseUnicodeValidation : Bool
caseUnicodeValidation =
    let
        -- 512 code points of a two-code-unit character plus check via review
        long =
            String.repeat 513 "a"

        model =
            drafted |> step (Bulk.SourceChanged long) |> Tuple.first

        ( reviewedModel, _ ) =
            step Bulk.ReviewRequested model

        nulModel =
            drafted |> step (Bulk.SourceChanged "a\u{0000}b") |> Tuple.first

        ( nulReviewed, _ ) =
            step Bulk.ReviewRequested nulModel

        emojiOk =
            loaded
                |> step (Bulk.SourceCheckChanged True)
                |> Tuple.first
                |> step (Bulk.SourceChanged (String.repeat 512 "😀"))
                |> Tuple.first
    in
    not (Bulk.reviewing reviewedModel)
        && not (Bulk.reviewing nulReviewed)
        && Bulk.reviewing (emojiOk |> step Bulk.ReviewRequested |> Tuple.first)


caseSaveSuccessOwns : Bool
caseSaveSuccessOwns =
    let
        ( settled, outs ) =
            step (Bulk.WriteCompleted 7 (Bulk.requestId saving) [ { id = "1", version = 0 }, { id = "2", version = 3 } ] (Just "new") Nothing (Bulk.captured saving) (Ok okResp)) saving
    in
    (outs |> List.map (\_ -> True)) == [ True ]
        && not (Bulk.saving settled)
        && Bulk.baseVersion "1" settled == Just 1
        && not (Bulk.changeSource settled)


case401 : Bool
case401 =
    let
        ( _, outs ) =
            step (Bulk.WriteCompleted 7 (Bulk.requestId saving) [ { id = "1", version = 0 }, { id = "2", version = 3 } ] (Just "new") Nothing (Bulk.captured saving) (Err (Http.BadStatus 401))) saving

        ( _, staleOuts ) =
            step (Bulk.WriteCompleted 3 (Bulk.requestId saving) [ { id = "1", version = 0 }, { id = "2", version = 3 } ] (Just "new") Nothing (Bulk.captured saving) (Err (Http.BadStatus 401))) saving
    in
    outs == [ Bulk.AccessRevoked ] && staleOuts == [ Bulk.StaleResponse [ "1", "2" ] ]


caseLockedClears : Bool
caseLockedClears =
    let
        lockedModel =
            Bulk.locked saving
    in
    Bulk.captured lockedModel == [] && not (Bulk.saving lockedModel) && Bulk.ownerIds lockedModel == []


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "bulk metadata read decoder requires fields and version zero", caseReadDecodeStrict )
    , ( "bulk metadata read encode and endpoint", caseEncodeRead )
    , ( "bulk metadata mixed values and unchecked omission", caseMixedAndUncheckedOmission )
    , ( "bulk metadata intentional clear", caseIntentionalClear )
    , ( "bulk metadata captures exact versions including zero", caseCaptureVersionsZero )
    , ( "bulk metadata over 100 disabled without truncation", caseOver100Disabled )
    , ( "bulk metadata selection access and request stale guards", caseStaleGuards )
    , ( "bulk metadata field change invalidates review", caseFieldChangeInvalidatesReview )
    , ( "bulk metadata conflict preserves draft and explicit reload recaptures", caseConflictPreservedReload )
    , ( "bulk metadata duplicate submit prevented", caseDuplicateSubmit )
    , ( "bulk metadata navigation mid-write captured targets", caseNavMidWrite )
    , ( "bulk metadata malformed and wrong receipts uncertain without retry", caseMalformedReceiptsUncertain )
    , ( "bulk metadata no-op versions validate", caseNoopVersions )
    , ( "bulk metadata monotonic source artist version pairs", caseMonotonicPairs )
    , ( "bulk metadata dirty base frozen against background reads", caseDirtyBaseFrozen )
    , ( "bulk metadata unicode code point and NUL validation", caseUnicodeValidation )
    , ( "bulk metadata save success settles owner", caseSaveSuccessOwns )
    , ( "bulk metadata 401 revokes and stale generation is stale", case401 )
    , ( "bulk metadata locked clears private snapshots", caseLockedClears )
    ]
