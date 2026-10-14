module RatingsTests exposing (allPassed, suite)

import Api.Ratings as RatingsApi
import Domain.Post as Post
import Domain.Rating as Rating exposing (Rating)
import Feature.Ratings as Ratings
import Http
import Json.Decode as Decode
import Json.Encode as Encode


ctx : List Ratings.TargetInfo -> Ratings.Context
ctx targets =
    Ratings.context "" (Just "tok") True 7 targets


target : String -> Rating -> Maybe Int -> Ratings.TargetInfo
target id rating version =
    { id = id, rating = rating, version = version }


summary : String -> Rating -> Maybe Int -> Post.PostSummary
summary id rating version =
    { id = id
    , previewUrl = "/m"
    , originalUrl = "/o"
    , mediaType = "image/jpeg"
    , width = 1
    , height = 1
    , tags = []
    , rating = rating
    , ratingVersion = version
    }


step : Ratings.Context -> Ratings.Msg -> Ratings.Model -> ( Ratings.Model, List Ratings.OutMsg )
step context msg model =
    let
        ( settled, _, outs ) =
            Ratings.update context msg model
    in
    ( settled, outs )


drafted : Ratings.Model
drafted =
    Ratings.selectionChanged [ summary "10" Rating.Unrated (Just 0) ] Ratings.init
        |> step (ctx [ target "10" Rating.Unrated (Just 0) ]) (Ratings.DraftChosen Rating.Safe)
        |> Tuple.first


savingModel : Ratings.Model
savingModel =
    step (ctx [ target "10" Rating.Unrated (Just 0) ]) Ratings.SaveRequested drafted |> Tuple.first


okResponse : List Post.RatingResult -> Post.RatingResponse
okResponse posts =
    { posts = posts }


caseEnumDefaults : Bool
caseEnumDefaults =
    let
        json =
            """{"id":"1","preview_url":"/m","original_url":"/o","media_type":"image/jpeg","width":1,"height":1}"""

        decoded =
            Decode.decodeString Post.decoder json
    in
    decoded == Ok { id = "1", previewUrl = "/m", originalUrl = "/o", mediaType = "image/jpeg", width = 1, height = 1, tags = [], rating = Rating.Unrated, ratingVersion = Nothing }


caseValidZero : Bool
caseValidZero =
    let
        json =
            """{"id":"1","preview_url":"/m","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"rating":"safe","rating_version":0}"""
    in
    (Decode.decodeString Post.decoder json |> Result.map (\post -> ( post.rating, post.ratingVersion ))) == Ok ( Rating.Safe, Just 0 )


caseUnknownRatingFails : Bool
caseUnknownRatingFails =
    let
        json =
            """{"id":"1","preview_url":"/m","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"rating":"bogus","rating_version":0}"""
    in
    case Decode.decodeString Post.decoder json of
        Err _ ->
            True

        Ok _ ->
            False


caseBadVersionsUnknown : Bool
caseBadVersionsUnknown =
    List.all
        (\fragment ->
            let
                json =
                    """{"id":"1","preview_url":"/m","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"rating":"safe","rating_version":""" ++ fragment ++ "}"
            in
            (Decode.decodeString Post.decoder json |> Result.map .ratingVersion) == Ok Nothing
        )
        [ "-1", "null", "\"1\"", "1.5" ]


caseDetailHistory : Bool
caseDetailHistory =
    let
        json =
            """{"id":"1","preview_url":"/m","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"source":"","artist":"","hash":"h","file_size":1,"created_at":"2026-01-01","tags":[],"rating":"explicit","rating_version":2,"rating_history":[{"version":2,"previous_rating":"safe","rating":"explicit","created_at":"2026-01-02"}]}"""

        noHistory =
            """{"id":"1","preview_url":"/m","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"source":"","artist":"","hash":"h","file_size":1,"created_at":"2026-01-01","tags":[]}"""
    in
    (Decode.decodeString Post.detailDecoder json
        |> Result.map (\detail -> ( detail.rating, detail.ratingVersion, List.map .version detail.ratingHistory ))
    )
        == Ok ( Rating.Explicit, Just 2, [ 2 ] )
        && (Decode.decodeString Post.detailDecoder noHistory |> Result.map .ratingHistory) == Ok []


caseEncodeExplicitZero : Bool
caseEncodeExplicitZero =
    Encode.encode 0 (RatingsApi.encodeRequest [ { id = "10", version = 0 } ] Rating.Safe)
        == """{"posts":[{"id":"10","version":0}],"rating":"safe"}"""


caseCaptureAtDraft : Bool
caseCaptureAtDraft =
    let
        ( saving, _ ) =
            step (ctx [ target "10" Rating.Unrated (Just 99) ]) Ratings.SaveRequested drafted
    in
    Ratings.capturedTargets saving == [ { id = "10", version = 0 } ] && Ratings.saving saving


caseUnknownVersionBlocks : Bool
caseUnknownVersionBlocks =
    let
        model =
            Ratings.selectionChanged [ summary "10" Rating.Unrated Nothing ] Ratings.init
                |> step (ctx [ target "10" Rating.Unrated Nothing ]) (Ratings.DraftChosen Rating.Safe)
                |> Tuple.first

        ( settled, _ ) =
            step (ctx [ target "10" Rating.Unrated Nothing ]) Ratings.SaveRequested model
    in
    not (Ratings.saving settled)
        && (Ratings.status settled |> Maybe.map (String.contains "unknown") |> Maybe.withDefault False)


caseBulkGuard : Bool
caseBulkGuard =
    let
        ids =
            List.range 1 101 |> List.map String.fromInt

        targets =
            List.map (\id -> target id Rating.Unrated (Just 0)) ids

        summaries =
            List.map (\id -> summary id Rating.Unrated (Just 0)) ids

        model =
            Ratings.selectionChanged summaries Ratings.init
                |> step (ctx targets) (Ratings.DraftChosen Rating.Safe)
                |> Tuple.first

        ( settled, _ ) =
            step (ctx targets) Ratings.SaveRequested model
    in
    not (Ratings.saving settled)
        && (Ratings.status settled |> Maybe.map (String.contains "100") |> Maybe.withDefault False)


caseDirtyBaseAfterBackground : Bool
caseDirtyBaseAfterBackground =
    let
        noted =
            Ratings.noteSummary (summary "10" Rating.Questionable (Just 1)) drafted
    in
    Ratings.draft noted
        == Just Rating.Safe
        && Ratings.baseVersion "10" noted
        == Just 0
        && Ratings.conflict noted


caseNavDuringPendingLateAck : Bool
caseNavDuringPendingLateAck =
    let
        moved =
            Ratings.selectionChanged [ summary "20" Rating.Safe (Just 3) ] savingModel

        ( settled, outs ) =
            step (ctx [ target "20" Rating.Safe (Just 3) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Ok (okResponse [ { id = "10", version = 1, rating = Rating.Safe, changed = True } ])))
                moved
    in
    outs
        == [ Ratings.Saved (okResponse [ { id = "10", version = 1, rating = Rating.Safe, changed = True } ]) [ "10" ] ]
        && Ratings.knownVersion "10" settled
        == Just 1
        && Ratings.ownerIds settled
        == [ "20" ]
        && Ratings.draft settled
        == Nothing
        && Ratings.status settled
        == Nothing


caseWrongReceiptsUncertain : Bool
caseWrongReceiptsUncertain =
    List.all
        (\response ->
            let
                ( settled, outs ) =
                    step (ctx [ target "10" Rating.Unrated (Just 0) ])
                        (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Ok response))
                        savingModel
            in
            outs == [ Ratings.Uncertain [ "10" ] ] && not (Ratings.saving settled) && Ratings.knownVersion "10" settled == Just 0
        )
        [ okResponse [ { id = "11", version = 1, rating = Rating.Safe, changed = True } ]
        , okResponse [ { id = "10", version = 1, rating = Rating.Explicit, changed = True } ]
        , okResponse [ { id = "10", version = 2, rating = Rating.Safe, changed = True } ]
        , okResponse [ { id = "10", version = 1, rating = Rating.Safe, changed = False } ]
        , okResponse
            [ { id = "10", version = 1, rating = Rating.Safe, changed = True }
            , { id = "10", version = 1, rating = Rating.Safe, changed = True }
            ]
        , okResponse []
        ]


caseNoopReceipt : Bool
caseNoopReceipt =
    let
        model =
            Ratings.selectionChanged [ summary "10" Rating.Safe (Just 4) ] Ratings.init
                |> step (ctx [ target "10" Rating.Safe (Just 4) ]) (Ratings.DraftChosen Rating.Safe)
                |> Tuple.first
                |> step (ctx [ target "10" Rating.Safe (Just 4) ]) Ratings.SaveRequested
                |> Tuple.first

        ( settled, outs ) =
            step (ctx [ target "10" Rating.Safe (Just 4) ])
                (Ratings.Completed 7 (Ratings.requestId model) [ { id = "10", version = 4 } ] Rating.Safe (Ok (okResponse [ { id = "10", version = 4, rating = Rating.Safe, changed = False } ])))
                model
    in
    (outs |> List.map (\_ -> True)) == [ True ] && Ratings.status settled == Just "Rating saved."


caseStaleCredential : Bool
caseStaleCredential =
    let
        ( settled, outs ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ])
                (Ratings.Completed 3 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Err (Http.BadStatus 401)))
                savingModel
    in
    outs == [ Ratings.StaleResponse [ "10" ] ] && settled == savingModel


caseCurrentAuth : Bool
caseCurrentAuth =
    let
        ( _, outs401 ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Err (Http.BadStatus 401)))
                savingModel

        ( settled403, outs403 ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Err (Http.BadStatus 403)))
                savingModel
    in
    outs401 == [ Ratings.AccessRevoked ] && outs403 == [] && Ratings.status settled403 == Just "This credential cannot change content ratings."


caseConflictAndReload : Bool
caseConflictAndReload =
    let
        ( conflicted, _ ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Err (Http.BadStatus 409)))
                savingModel

        ( reloading, outs ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ]) Ratings.ReloadRequested conflicted

        rebased =
            Ratings.noteSummary (summary "10" Rating.Questionable (Just 1)) reloading
    in
    Ratings.draft conflicted
        == Just Rating.Safe
        && Ratings.conflict conflicted
        && outs
        == [ Ratings.ReloadTargets [ "10" ] ]
        && Ratings.baseVersion "10" rebased
        == Just 1
        && not (Ratings.conflict rebased)
        && Ratings.draft rebased
        == Just Rating.Safe


caseUncertainTransport : Bool
caseUncertainTransport =
    List.all
        (\error ->
            let
                ( _, outs ) =
                    step (ctx [ target "10" Rating.Unrated (Just 0) ])
                        (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Err error))
                        savingModel
            in
            outs == [ Ratings.Uncertain [ "10" ] ]
        )
        [ Http.BadStatus 503, Http.Timeout, Http.NetworkError, Http.BadBody "bad" ]


caseOlderCannotUndoAck : Bool
caseOlderCannotUndoAck =
    let
        ( saved, _ ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Ok (okResponse [ { id = "10", version = 1, rating = Rating.Safe, changed = True } ])))
                savingModel

        noted =
            Ratings.noteSummary (summary "10" Rating.Unrated (Just 0)) saved
    in
    Ratings.knownVersion "10" noted == Just 1 && Ratings.knownRating "10" noted == Just Rating.Safe


caseDisplayMixed : Bool
caseDisplayMixed =
    Ratings.display [ summary "1" Rating.Safe (Just 0), summary "2" Rating.Explicit (Just 0) ] Ratings.init
        == Ratings.Mixed
        && Ratings.display [ summary "1" Rating.Safe (Just 0) ] Ratings.init
        == Ratings.Uniform Rating.Safe


caseDelayedReceiptAfterNewerRead : Bool
caseDelayedReceiptAfterNewerRead =
    let
        receipt =
            okResponse [ { id = "10", version = 1, rating = Rating.Safe, changed = True } ]

        noted =
            Ratings.noteSummary (summary "10" Rating.Explicit (Just 2)) savingModel

        ( settled, outs ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Ok receipt))
                noted

        movedAway =
            Ratings.selectionChanged [ summary "20" Rating.Safe (Just 3) ] settled

        freshDraftBase =
            Ratings.selectionChanged [ summary "10" Rating.Safe (Just 1) ] movedAway
    in
    outs
        == [ Ratings.Saved receipt [ "10" ] ]
        && Ratings.knownVersion "10" settled
        == Just 2
        && Ratings.knownRating "10" settled
        == Just Rating.Explicit
        && Ratings.draft settled
        == Just Rating.Safe
        && Ratings.baseVersion "10" settled
        == Just 0
        && Ratings.conflict settled
        && Ratings.status settled
        /= Just "Rating saved."
        && Ratings.display [ summary "10" Rating.Safe (Just 1) ] movedAway
        == Ratings.Uniform Rating.Explicit
        && Ratings.baseVersion "10" freshDraftBase
        == Just 2


caseOlderSummaryCaptureAndDisplay : Bool
caseOlderSummaryCaptureAndDisplay =
    let
        known =
            Ratings.selectionChanged [ summary "10" Rating.Explicit (Just 2) ] Ratings.init

        recaptured =
            Ratings.selectionChanged [ summary "10" Rating.Safe (Just 1) ] known

        ( draftedAgain, _ ) =
            step (ctx [ target "10" Rating.Safe (Just 1) ]) (Ratings.DraftChosen Rating.Questionable) recaptured
    in
    Ratings.knownVersion "10" recaptured
        == Just 2
        && Ratings.knownRating "10" recaptured
        == Just Rating.Explicit
        && Ratings.baseVersion "10" recaptured
        == Just 2
        && Ratings.display [ summary "10" Rating.Safe (Just 1) ] recaptured
        == Ratings.Uniform Rating.Explicit
        && Ratings.baseVersion "10" draftedAgain
        == Just 2


caseLegacyMissingPreservesKnownPair : Bool
caseLegacyMissingPreservesKnownPair =
    let
        known =
            Ratings.selectionChanged [ summary "10" Rating.Explicit (Just 2) ] Ratings.init

        noted =
            Ratings.noteSummary (summary "10" Rating.Unrated Nothing) known

        recaptured =
            Ratings.selectionChanged [ summary "10" Rating.Unrated Nothing ] noted

        legacyOnly =
            Ratings.noteSummary (summary "10" Rating.Unrated Nothing) Ratings.init
    in
    Ratings.knownVersion "10" noted
        == Just 2
        && Ratings.knownRating "10" noted
        == Just Rating.Explicit
        && Ratings.baseVersion "10" recaptured
        == Just 2
        && Ratings.knownVersion "10" legacyOnly
        == Nothing


caseNavLateAckKeepsOtherDraft : Bool
caseNavLateAckKeepsOtherDraft =
    let
        receipt =
            okResponse [ { id = "10", version = 1, rating = Rating.Safe, changed = True } ]

        noted =
            Ratings.noteSummary (summary "10" Rating.Explicit (Just 2)) savingModel

        moved =
            Ratings.selectionChanged [ summary "20" Rating.Safe (Just 3) ] noted

        draftedOther =
            step (ctx [ target "20" Rating.Safe (Just 3) ]) (Ratings.DraftChosen Rating.Questionable) moved
                |> Tuple.first

        ( settled, outs ) =
            step (ctx [ target "20" Rating.Safe (Just 3) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Ok receipt))
                draftedOther
    in
    outs
        == [ Ratings.Saved receipt [ "10" ] ]
        && Ratings.knownVersion "10" settled
        == Just 2
        && Ratings.knownRating "10" settled
        == Just Rating.Explicit
        && Ratings.ownerIds settled
        == [ "20" ]
        && Ratings.draft settled
        == Just Rating.Questionable
        && Ratings.baseVersion "20" settled
        == Just 3
        && Ratings.baseVersion "10" settled
        == Nothing


caseLockedSave : Bool
caseLockedSave =
    let
        lockedCtx =
            Ratings.context "" Nothing False 7 [ target "10" Rating.Unrated (Just 0) ]

        ( settled, _ ) =
            step lockedCtx Ratings.SaveRequested drafted
    in
    Ratings.status settled == Just "Unlock to change content ratings." && not (Ratings.saving settled)


caseNewerPagePreventsOlderDetail : Bool
caseNewerPagePreventsOlderDetail =
    let
        existing =
            Ratings.noteSummary (summary "10" Rating.Safe (Just 1)) Ratings.init

        pageLearned =
            List.foldl Ratings.noteSummary existing [ summary "10" Rating.Explicit (Just 2) ]

        oldRead =
            Ratings.noteSummary (summary "10" Rating.Safe (Just 1)) pageLearned

        selected =
            Ratings.selectionChanged [ summary "10" Rating.Safe (Just 1) ] oldRead
    in
    Ratings.knownVersion "10" selected == Just 2
        && Ratings.knownRating "10" selected == Just Rating.Explicit
        && Ratings.baseVersion "10" selected == Just 2
        && Ratings.display [ summary "10" Rating.Safe (Just 1) ] selected == Ratings.Uniform Rating.Explicit


caseConflictCannotResubmitBeforeReload : Bool
caseConflictCannotResubmitBeforeReload =
    let
        ( conflicted, _ ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ])
                (Ratings.Completed 7 (Ratings.requestId savingModel) [ { id = "10", version = 0 } ] Rating.Safe (Err (Http.BadStatus 409)))
                savingModel

        ( stopped, outs ) =
            step (ctx [ target "10" Rating.Unrated (Just 0) ]) Ratings.SaveRequested conflicted
    in
    not (Ratings.saving stopped)
        && outs == []
        && Ratings.requestId stopped == Ratings.requestId conflicted
        && Ratings.draft stopped == Just Rating.Safe
        && (Ratings.status stopped |> Maybe.map (String.contains "Reload") |> Maybe.withDefault False)


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "rating missing rating defaults unrated with unknown version", caseEnumDefaults )
    , ( "rating present explicit zero is a valid version", caseValidZero )
    , ( "rating present unknown rating fails decoding", caseUnknownRatingFails )
    , ( "rating bad versions never become valid zero", caseBadVersionsUnknown )
    , ( "rating detail history decodes and defaults empty", caseDetailHistory )
    , ( "rating bulk request encodes explicit zero", caseEncodeExplicitZero )
    , ( "rating versions captured when draft is established", caseCaptureAtDraft )
    , ( "rating unknown version blocks writes", caseUnknownVersionBlocks )
    , ( "rating bulk save is capped at 100", caseBulkGuard )
    , ( "rating dirty base survives background read", caseDirtyBaseAfterBackground )
    , ( "rating navigation during pending keeps late ack on captured post", caseNavDuringPendingLateAck )
    , ( "rating wrong receipts are uncertain", caseWrongReceiptsUncertain )
    , ( "rating no-op receipt keeps base version", caseNoopReceipt )
    , ( "rating stale credential response cannot revoke fresh unlock", caseStaleCredential )
    , ( "rating current 401 revokes and 403 settles clearly", caseCurrentAuth )
    , ( "rating conflict cannot resubmit until explicit reload", caseConflictCannotResubmitBeforeReload )
    , ( "rating conflict keeps draft and explicit reload rebases", caseConflictAndReload )
    , ( "rating uncertain transports reconcile without declaring save", caseUncertainTransport )
    , ( "rating older read cannot undo acknowledgement", caseOlderCannotUndoAck )
    , ( "rating higher page pair protects against delayed older Inspector read", caseNewerPagePreventsOlderDetail )
    , ( "rating display reports mixed and uniform selections", caseDisplayMixed )
    , ( "rating delayed receipt after newer read keeps newer pair and dirty draft", caseDelayedReceiptAfterNewerRead )
    , ( "rating older summary cannot recapture base or display", caseOlderSummaryCaptureAndDisplay )
    , ( "rating legacy missing pair preserves known pair and never invents zero", caseLegacyMissingPreservesKnownPair )
    , ( "rating late ack after navigation keeps other post draft", caseNavLateAckKeepsOtherDraft )
    , ( "rating locked save asks to unlock", caseLockedSave )
    ]
