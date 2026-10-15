module SimilarityTests exposing (allPassed, suite)

import Api.Similarity as SimApi
import Domain.Post as Post
import Domain.Rating as Rating
import Domain.Similarity as Sim
import Feature.Similarity as SimF
import Http
import Json.Decode as Decode


ctx : SimF.Context
ctx =
    SimF.context "" Nothing True 7


lockedCtx : SimF.Context
lockedCtx =
    SimF.context "" Nothing False 7


summary : String -> Post.PostSummary
summary id =
    { id = id, previewUrl = "/p" ++ id, originalUrl = "/o" ++ id, mediaType = "image/png", width = 10, height = 20, tags = [], rating = Rating.Unrated, ratingVersion = Nothing }


match : String -> Int -> Sim.SimilarMatch
match id distance =
    { post = summary id, distance = distance }


step : SimF.Msg -> SimF.Model -> ( SimF.Model, List SimF.OutMsg )
step msg model =
    let
        ( next, _, outs ) =
            SimF.update ctx msg model
    in
    ( next, outs )


stepWith : SimF.Context -> SimF.Msg -> SimF.Model -> ( SimF.Model, List SimF.OutMsg )
stepWith context msg model =
    let
        ( next, _, outs ) =
            SimF.update context msg model
    in
    ( next, outs )


opened : SimF.Model
opened =
    SimF.init |> step (SimF.OpenRequested (summary "10")) |> Tuple.first


loaded : SimF.Model
loaded =
    opened
        |> step (SimF.SimilarLoaded 7 (SimF.requestId opened) "10" 8 False (Ok { postId = "10", algorithm = "dhash-v1", indexed = True, matches = [ match "11" 0, match "12" 3, match "13" 8 ], nextCursor = Just "c1" }))
        |> Tuple.first


unindexed : SimF.Model
unindexed =
    opened
        |> step (SimF.SimilarLoaded 7 (SimF.requestId opened) "10" 8 False (Ok { postId = "10", algorithm = "dhash-v1", indexed = False, matches = [], nextCursor = Nothing }))
        |> Tuple.first


isErr : Result e a -> Bool
isErr result =
    case result of
        Err _ ->
            True

        Ok _ ->
            False


caseDecoderStrict : Bool
caseDecoderStrict =
    let
        good =
            """{"post_id":"10","algorithm":"dhash-v1","indexed":true,"matches":[{"post":{"id":"11","preview_url":"/p","original_url":"/o","media_type":"image/png","width":1,"height":2},"distance":0}],"next_cursor":null}"""

        badAlgo =
            String.replace "dhash-v1" "phash-v1" good

        badId =
            String.replace "\"post_id\":\"10\"" "\"post_id\":\"010\"" good

        badDistance =
            String.replace "\"distance\":0" "\"distance\":17" good

        missingIndexed =
            String.replace "\"indexed\":true," "" good
    in
    (Decode.decodeString Sim.similarPageDecoder good |> Result.map (\p -> ( p.postId, List.map .distance p.matches ))) == Ok ( "10", [ 0 ] )
        && isErr (Decode.decodeString Sim.similarPageDecoder badAlgo)
        && isErr (Decode.decodeString Sim.similarPageDecoder badId)
        && isErr (Decode.decodeString Sim.similarPageDecoder badDistance)
        && isErr (Decode.decodeString Sim.similarPageDecoder missingIndexed)
        && Sim.isCanonicalId "10"
        && not (Sim.isCanonicalId "010")
        && not (Sim.isCanonicalId "0")


caseStatusAndIndexDecoders : Bool
caseStatusAndIndexDecoders =
    let
        statusJson =
            """{"algorithm":"dhash-v1","indexed_count":1,"eligible_count":3,"posts":[{"id":"1","indexed":true,"job_id":null,"status":null},{"id":"2","indexed":false,"job_id":"j2","status":"failed"}],"next_cursor":"x"}"""

        indexJson =
            """{"posts":[{"id":"2","job_id":"j2","status":"pending"}]}"""
    in
    (Decode.decodeString Sim.statusPageDecoder statusJson |> Result.map (\p -> ( p.indexedCount, p.eligibleCount, List.map .id p.posts ))) == Ok ( 1, 3, [ "1", "2" ] )
        && (Decode.decodeString Sim.indexResponseDecoder indexJson |> Result.map (List.map .jobId << .posts)) == Ok [ "j2" ]
        && isErr (Decode.decodeString Sim.statusPageDecoder (String.replace "dhash-v1" "x" statusJson))


caseEndpoints : Bool
caseEndpoints =
    SimApi.similarEndpoint "" "10" 8 30 Nothing
        == "/api/posts/10/similar?distance=8&limit=30"
        && SimApi.similarEndpoint "https://x/" "10" 0 60 (Just "a b&c")
        == "https://x/api/posts/10/similar?distance=0&limit=60&cursor=a%20b%26c"
        && SimApi.statusEndpoint "" 100 (Just "a b")
        == "/api/posts/similarity/status?limit=100&cursor=a%20b"


caseStaleIgnored : Bool
caseStaleIgnored =
    let
        page =
            Ok { postId = "10", algorithm = "dhash-v1", indexed = True, matches = [ match "99" 1 ], nextCursor = Nothing }

        wrongGen =
            loaded |> step (SimF.SimilarLoaded 3 (SimF.requestId loaded) "10" 8 False page) |> Tuple.first

        wrongAnchor =
            loaded |> step (SimF.SimilarLoaded 7 (SimF.requestId loaded) "11" 8 False page) |> Tuple.first

        wrongThreshold =
            loaded |> step (SimF.SimilarLoaded 7 (SimF.requestId loaded) "10" 4 False page) |> Tuple.first

        wrongCanonical =
            opened |> step (SimF.SimilarLoaded 7 (SimF.requestId opened) "10" 8 False (Ok { postId = "11", algorithm = "dhash-v1", indexed = True, matches = [ match "99" 1 ], nextCursor = Nothing })) |> Tuple.first
    in
    List.map (.id << .post) (SimF.matches wrongGen) == [ "11", "12", "13" ]
        && List.map (.id << .post) (SimF.matches wrongAnchor) == [ "11", "12", "13" ]
        && List.map (.id << .post) (SimF.matches wrongThreshold) == [ "11", "12", "13" ]
        && SimF.status wrongCanonical /= Nothing
        && SimF.matches wrongCanonical == []


caseThresholdExplicitReload : Bool
caseThresholdExplicitReload =
    let
        changed =
            loaded |> step (SimF.ThresholdChanged 99) |> Tuple.first

        reloaded =
            changed |> step SimF.ReloadRequested |> Tuple.first
    in
    SimF.currentThreshold changed == 8
        && SimF.currentThreshold reloaded == 16
        && SimF.isBusy reloaded


caseSelectionMaxAndNoAutoTrash : Bool
caseSelectionMaxAndNoAutoTrash =
    let
        opening =
            SimF.init |> step (SimF.OpenRequested (summary "1")) |> Tuple.first

        many =
            List.map (\n -> match (String.fromInt n) 0) (List.range 2 102)

        loadedMany =
            opening
                |> step (SimF.SimilarLoaded 7 (SimF.requestId opening) "1" 8 False (Ok { postId = "1", algorithm = "dhash-v1", indexed = True, matches = many, nextCursor = Nothing }))
                |> Tuple.first

        chosen =
            List.foldl (\id model -> step (SimF.MatchToggled id True) model |> Tuple.first) loadedMany (List.map String.fromInt (List.range 2 102))

        ( _, noChoiceOuts ) =
            step SimF.TrashRequested loaded
    in
    SimF.selectedIds loaded == []
        && noChoiceOuts == []
        && List.length (SimF.selectedIds chosen) == 100
        && SimF.status chosen /= Nothing


caseExplicitTrashLockedAndCompare : Bool
caseExplicitTrashLockedAndCompare =
    let
        chosen =
            loaded |> step (SimF.MatchToggled "12" True) |> Tuple.first

        ( _, outs ) =
            step SimF.TrashRequested chosen

        ( lockedModel, lockedOuts ) =
            stepWith lockedCtx SimF.TrashRequested chosen

        ( _, compareOuts ) =
            step (SimF.CompareRequested "13") loaded

        ( _, badCompareOuts ) =
            step (SimF.CompareRequested "99") loaded
    in
    outs == [ SimF.TrashSelected [ "12" ] ]
        && lockedOuts == []
        && SimF.status lockedModel /= Nothing
        && compareOuts == [ SimF.CompareCopies (summary "10") (summary "13") (summary "10" :: SimF.members loaded) ]
        && badCompareOuts == []
        && SimF.selectedIds (SimF.locked chosen) == []


caseUnindexedAndLockedIndex : Bool
caseUnindexedAndLockedIndex =
    let
        ( lockedModel, lockedOuts ) =
            stepWith lockedCtx SimF.IndexAnchorRequested unindexed

        ( posting, _ ) =
            step SimF.IndexAnchorRequested unindexed
    in
    SimF.status unindexed /= Nothing
        && lockedOuts == []
        && not (SimF.isBusy lockedModel)
        && SimF.isBusy posting


caseUncertainBlocked : Bool
caseUncertainBlocked =
    let
        posting =
            unindexed |> step SimF.IndexAnchorRequested |> Tuple.first

        ( uncertainModel, outs ) =
            step (SimF.IndexAnchorLoaded 7 (SimF.requestId posting) (Err Http.Timeout)) posting

        ( blocked, blockedOuts ) =
            step SimF.IndexAnchorRequested uncertainModel

        reviewed =
            blocked |> step SimF.UncertainReviewed |> Tuple.first

        ( revokedModel, revokedOuts ) =
            step (SimF.IndexAnchorLoaded 7 (SimF.requestId posting) (Err (Http.BadStatus 401))) posting
    in
    outs == [ SimF.ResultUncertain "anchor index" ]
        && SimF.resultUncertain uncertainModel
        && blockedOuts == []
        && SimF.resultUncertain blocked
        && not (SimF.resultUncertain reviewed)
        && revokedOuts == [ SimF.AccessRevoked ]
        && not (SimF.resultUncertain revokedModel)


caseCancelStopsNextBatch : Bool
caseCancelStopsNextBatch =
    let
        scanning =
            loaded |> step SimF.IndexLibraryRequested |> Tuple.first

        gen =
            SimF.queueGeneration scanning

        queuedScan =
            scanning
                |> step (SimF.StatusScanLoaded gen (Ok { algorithm = "dhash-v1", indexedCount = 0, eligibleCount = 3, posts = [ { id = "21", indexed = False, jobId = Nothing, status = Nothing }, { id = "22", indexed = False, jobId = Just "j", status = Just "failed" }, { id = "23", indexed = True, jobId = Nothing, status = Nothing } ], nextCursor = Nothing }))
                |> Tuple.first

        cancelled =
            queuedScan |> step SimF.CancelIndexLibraryRequested |> Tuple.first

        lateBatch =
            cancelled
                |> step (SimF.IndexBatchLoaded gen [ "21" ] (Ok { posts = [ { id = "21", jobId = "j21", status = "pending" } ] }))
                |> Tuple.first
    in
    SimF.indexingLibrary queuedScan
        && not (SimF.indexingLibrary cancelled)
        && not (SimF.indexingLibrary lateBatch)
        && SimF.status cancelled /= Nothing


caseVisibilityTrashRestoreReload : Bool
caseVisibilityTrashRestoreReload =
    let
        ( afterVisibility, _ ) =
            step SimF.VisibilityChanged loaded

        chosen =
            loaded |> step (SimF.MatchToggled "11" True) |> Tuple.first

        ( afterTrash, _ ) =
            step (SimF.TrashApplied [ "11" ]) chosen

        ( afterRestore, _ ) =
            step (SimF.RestoreApplied [ "11" ]) loaded

        opening =
            SimF.init |> step (SimF.OpenRequested (summary "10")) |> Tuple.first

        gone =
            opening |> step (SimF.SimilarLoaded 7 (SimF.requestId opening) "10" 8 False (Err (Http.BadStatus 404))) |> Tuple.first

        anchorGone =
            loaded |> step (SimF.TrashApplied [ "10" ]) |> Tuple.first
    in
    SimF.isBusy afterVisibility
        && List.map (.id << .post) (SimF.matches afterTrash) == [ "12", "13" ]
        && SimF.selectedIds afterTrash == []
        && SimF.isBusy afterTrash
        && SimF.isBusy afterRestore
        && SimF.anchorId gone == Nothing
        && (SimF.status gone |> Maybe.map (String.contains "no longer available") |> Maybe.withDefault False)
        && SimF.anchorId anchorGone == Nothing


caseErrorPreservesAndCloseClears : Bool
caseErrorPreservesAndCloseClears =
    let
        reloading =
            loaded |> step SimF.ReloadRequested |> Tuple.first

        failed =
            reloading |> step (SimF.SimilarLoaded 7 (SimF.requestId reloading) "10" 8 False (Err (Http.BadStatus 503))) |> Tuple.first

        chosen =
            loaded |> step (SimF.MatchToggled "11" True) |> Tuple.first

        closed =
            chosen |> step SimF.CloseRequested |> Tuple.first
    in
    List.map (.id << .post) (SimF.matches failed) == [ "11", "12", "13" ]
        && SimF.status failed /= Nothing
        && not (SimF.isOpen closed)
        && SimF.selectedIds closed == []
        && SimF.anchorId closed == Nothing


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "similarity decoder strict required fields and canonical ids", caseDecoderStrict )
    , ( "similarity status and index decoders", caseStatusAndIndexDecoders )
    , ( "similarity endpoints encode threshold and cursor", caseEndpoints )
    , ( "similarity stale anchor threshold and canonical responses ignored", caseStaleIgnored )
    , ( "similarity threshold applies only on explicit reload", caseThresholdExplicitReload )
    , ( "similarity selection independent max 100 and no auto trash", caseSelectionMaxAndNoAutoTrash )
    , ( "similarity explicit trash locked gate and compare", caseExplicitTrashLockedAndCompare )
    , ( "similarity unindexed anchor and locked index disabled", caseUnindexedAndLockedIndex )
    , ( "similarity uncertain index queue blocked until review", caseUncertainBlocked )
    , ( "similarity cancelled batch does not queue next", caseCancelStopsNextBatch )
    , ( "similarity visibility trash restore bounded reload", caseVisibilityTrashRestoreReload )
    , ( "similarity error preserves view and close clears choice", caseErrorPreservesAndCloseClears )
    ]
