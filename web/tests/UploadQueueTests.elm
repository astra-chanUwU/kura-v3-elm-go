module UploadQueueTests exposing (allPassed, suite)

import Api.Job as Job
import Domain.Post as Post
import Domain.Sequence as Sequence
import Feature.UploadQueue as UploadQueue exposing (Context, Entry, FileInfo, Model, Msg(..), OutMsg(..))
import Http
import Json.Decode as Decode


info : String -> String -> Int -> FileInfo
info name mime size =
    { name = name, mime = mime, size = size }


caseJpegAccepted : Bool
caseJpegAccepted =
    UploadQueue.validateFile (info "photo.jpg" "image/jpeg" 1024) == Ok ()


casePngAccepted : Bool
casePngAccepted =
    UploadQueue.validateFile (info "photo.png" "image/png" 2048) == Ok ()


caseGifAccepted : Bool
caseGifAccepted =
    UploadQueue.validateFile (info "anim.gif" "image/gif" 4096) == Ok ()


caseBadMimeRejected : Bool
caseBadMimeRejected =
    case UploadQueue.validateFile (info "notes.txt" "text/plain" 100) of
        Err _ ->
            True

        Ok () ->
            False


caseEmptyMimeFallsBackToExtension : Bool
caseEmptyMimeFallsBackToExtension =
    UploadQueue.validateFile (info "photo.JPG" "" 100) == Ok ()


caseEmptyMimeUnknownExtensionRejected : Bool
caseEmptyMimeUnknownExtensionRejected =
    case UploadQueue.validateFile (info "archive.zip" "" 100) of
        Err _ ->
            True

        Ok () ->
            False


caseOversizedRejected : Bool
caseOversizedRejected =
    case UploadQueue.validateFile (info "huge.png" "image/png" (25 * 1024 * 1024 + 1)) of
        Err message ->
            String.contains "25 MiB" message

        Ok () ->
            False


caseEmptyRejected : Bool
caseEmptyRejected =
    case UploadQueue.validateFile (info "empty.png" "image/png" 0) of
        Err _ ->
            True

        Ok () ->
            False


caseTagsParsed : Bool
caseTagsParsed =
    UploadQueue.parseTags "cat, demo,,cat ,  portrait " == [ "cat", "demo", "portrait" ]


caseBytesFormatted : Bool
caseBytesFormatted =
    UploadQueue.formatBytes 512 == "512 B"
        && UploadQueue.formatBytes 2048 == "2.0 KiB"
        && UploadQueue.formatBytes (25 * 1024 * 1024) == "25.0 MiB"


caseTerminalJobs : Bool
caseTerminalJobs =
    Job.isTerminal "succeeded"
        && Job.isTerminal "failed"
        && Job.isTerminal "cancelled"
        && not (Job.isTerminal "pending")
        && not (Job.isTerminal "running")
        && not (Job.isTerminal "")


casePhaseLabels : Bool
casePhaseLabels =
    UploadQueue.phaseLabel UploadQueue.Queued == "Queued"
        && UploadQueue.phaseLabel (UploadQueue.Saving 1) == "Saving"
        && UploadQueue.thumbLabel (UploadQueue.ThumbWaiting 3) == "Thumbnail: processing…"
        && UploadQueue.thumbLabel UploadQueue.ThumbReady == "Thumbnail: ready"


summary : String -> String -> Post.PostSummary
summary id preview =
    { id = id
    , previewUrl = preview
    , originalUrl = "/media/" ++ id
    , mediaType = "image/jpeg"
    , width = 8
    , height = 8
    , tags = []
    }


caseRefreshMemberInPlace : Bool
caseRefreshMemberInPlace =
    let
        sequence =
            Sequence.fromList [ summary "1" "a", summary "2" "b" ]
                |> Sequence.refreshSummary (summary "1" "a2")
    in
    Sequence.ids sequence == [ "1", "2" ]
        && (Sequence.find "1" sequence |> Maybe.map .previewUrl) == Just "a2"


caseRefreshNeverInjects : Bool
caseRefreshNeverInjects =
    let
        before =
            Sequence.fromList [ summary "1" "a", summary "2" "b" ]

        after =
            Sequence.refreshSummary (summary "9" "z") before
    in
    Sequence.ids after == [ "1", "2" ]
        && Sequence.length after == Sequence.length before
        && Sequence.find "9" after == Nothing


testEntry : Int -> UploadQueue.Phase -> Entry
testEntry id phase =
    { id = id
    , name = "photo-" ++ String.fromInt id ++ ".jpg"
    , size = 1024
    , mime = "image/jpeg"
    , tags = []
    , source = ""
    , artist = ""
    , phase = phase
    }


testModel : List Entry -> Model
testModel entries =
    let
        base =
            UploadQueue.init
    in
    { base | entries = entries, nextId = List.length entries }


openCtx : Context
openCtx =
    UploadQueue.context "" Nothing True 1


lockedCtx : Context
lockedCtx =
    UploadQueue.context "" Nothing False 1


dismissEntries : Model -> Int -> List Entry
dismissEntries model entryId =
    UploadQueue.update openCtx (DismissEntry entryId) model
        |> (\( next, _, _ ) -> next.entries)


phasesOf : Model -> List String
phasesOf model =
    List.map (\entry -> UploadQueue.phaseLabel entry.phase) model.entries


outsOf : ( Model, Cmd Msg, List OutMsg ) -> List String
outsOf ( _, _, outs ) =
    List.map outLabel outs


outLabel : OutMsg -> String
outLabel out =
    case out of
        UploadConfirmed _ ->
            "UploadConfirmed"

        ThumbCompleted _ ->
            "ThumbCompleted"

        OpenPost _ ->
            "OpenPost"

        CredentialRejected generation ->
            "CredentialRejected " ++ String.fromInt generation


decodeTestDetail : Result String Post.PostDetail
decodeTestDetail =
    case Decode.decodeString Post.detailDecoder detailJson of
        Ok detail ->
            Ok detail

        Err err ->
            Err (Decode.errorToString err)


caseLockedPumpHoldsQueued : Bool
caseLockedPumpHoldsQueued =
    let
        model =
            testModel [ testEntry 0 UploadQueue.Queued, testEntry 1 UploadQueue.Queued ]

        ( next, _, outs ) =
            UploadQueue.update lockedCtx (DismissEntry 0) model
    in
    List.map .id next.entries == [ 1 ] && phasesOf next == [ "Queued" ] && outs == []


caseUnlockedPumpAttemptsQueued : Bool
caseUnlockedPumpAttemptsQueued =
    -- Without the picked file the attempt fails the entry instead of
    -- stalling; the point is the unlocked queue does not hold still.
    let
        model =
            testModel [ testEntry 0 UploadQueue.Queued, testEntry 1 UploadQueue.Queued ]

        ( next, _, _ ) =
            UploadQueue.update openCtx (DismissEntry 0) model
    in
    List.map .id next.entries == [ 1 ] && phasesOf next == [ "Failed" ]


caseResumeLockedHolds : Bool
caseResumeLockedHolds =
    let
        model =
            testModel [ testEntry 0 UploadQueue.Queued ]

        ( next, _, outs ) =
            UploadQueue.resume lockedCtx model
    in
    phasesOf next == [ "Queued" ] && outs == []


caseFinishQueuedIgnored : Bool
caseFinishQueuedIgnored =
    case decodeTestDetail of
        Err _ ->
            False

        Ok detail ->
            let
                model =
                    testModel [ testEntry 0 UploadQueue.Queued ]

                ( next, _, outs ) =
                    UploadQueue.update openCtx (UploadFinished 0 1 (Ok detail)) model
            in
            phasesOf next == [ "Queued" ] && outs == []


caseFinishTimeoutHonest : Bool
caseFinishTimeoutHonest =
    let
        model =
            testModel [ testEntry 0 (UploadQueue.Saving 1) ]

        ( next, _, outs ) =
            UploadQueue.update openCtx (UploadFinished 0 1 (Err Http.Timeout)) model
    in
    case next.entries of
        [ entry ] ->
            case entry.phase of
                UploadQueue.Failed message ->
                    String.contains "may already exist" message && outs == []

                _ ->
                    False

        _ ->
            False


caseFinishUnauthorizedHoldsQueue : Bool
caseFinishUnauthorizedHoldsQueue =
    let
        model =
            testModel
                [ testEntry 0 (UploadQueue.Saving 1)
                , testEntry 1 UploadQueue.Queued
                ]

        ( next, _, outs ) =
            UploadQueue.update openCtx (UploadFinished 0 1 (Err (Http.BadStatus 401))) model
    in
    phasesOf next == [ "Failed", "Queued" ]
        && outsOf ( next, Cmd.none, outs ) == [ "CredentialRejected 1" ]


caseStaleUnauthorizedCarriesRequestGeneration : Bool
caseStaleUnauthorizedCarriesRequestGeneration =
    let
        renewedCtx =
            UploadQueue.context "" Nothing True 2

        model =
            testModel [ testEntry 0 (UploadQueue.Saving 1) ]

        ( _, _, outs ) =
            UploadQueue.update renewedCtx (UploadFinished 0 1 (Err (Http.BadStatus 401))) model
    in
    outsOf ( model, Cmd.none, outs ) == [ "CredentialRejected 1" ]


caseFinishConfirmedStands : Bool
caseFinishConfirmedStands =
    case decodeTestDetail of
        Err _ ->
            False

        Ok detail ->
            let
                model =
                    testModel [ testEntry 0 (UploadQueue.Saving 1) ]

                ( next, _, outs ) =
                    UploadQueue.update lockedCtx (UploadFinished 0 1 (Ok detail)) model
            in
            -- A confirmed upload stands even when the browser locked
            -- mid-flight: lock is not rollback.
            case next.entries of
                [ entry ] ->
                    case entry.phase of
                        UploadQueue.Uploaded uploaded ->
                            uploaded.postId == "42" && outsOf ( next, Cmd.none, outs ) == [ "UploadConfirmed" ]

                        _ ->
                            False

                _ ->
                    False


caseDismissUploadingRefused : Bool
caseDismissUploadingRefused =
    let
        model =
            testModel [ testEntry 0 (UploadQueue.Uploading { sent = 10, size = 100, accessGeneration = 1 }) ]
    in
    dismissEntries model 0 == model.entries


caseDismissSavingRefused : Bool
caseDismissSavingRefused =
    let
        model =
            testModel [ testEntry 0 (UploadQueue.Saving 1), testEntry 1 UploadQueue.Queued ]
    in
    dismissEntries model 0 == model.entries


caseDismissQueuedRemoves : Bool
caseDismissQueuedRemoves =
    let
        model =
            testModel [ testEntry 0 UploadQueue.Queued, testEntry 1 UploadQueue.Queued ]
    in
    List.map .id (dismissEntries model 0) == [ 1 ]


caseDismissUnknownNoop : Bool
caseDismissUnknownNoop =
    let
        model =
            testModel [ testEntry 0 UploadQueue.Queued ]
    in
    dismissEntries model 99 == model.entries


caseDismissiblePhases : Bool
caseDismissiblePhases =
    UploadQueue.isDismissible (testEntry 0 UploadQueue.Queued)
        && UploadQueue.isDismissible (testEntry 0 (UploadQueue.Rejected "nope"))
        && UploadQueue.isDismissible (testEntry 0 (UploadQueue.Failed "nope"))
        && not (UploadQueue.isDismissible (testEntry 0 (UploadQueue.Uploading { sent = 0, size = 1, accessGeneration = 1 })))
        && not (UploadQueue.isDismissible (testEntry 0 (UploadQueue.Saving 1)))


detailJson : String
detailJson =
    """{"id":"42","preview_url":"/media/p","original_url":"/media/o","media_type":"image/png","width":4,"height":4,"source":"s","artist":"a","hash":"h","file_size":10,"created_at":"2026-01-01","tags":[],"derivative_job_id":"7","derivative_status":"pending"}"""


caseDerivativeDecodes : Bool
caseDerivativeDecodes =
    case Decode.decodeString Post.detailDecoder detailJson of
        Ok detail ->
            detail.derivativeJobId == Just "7" && detail.derivativeStatus == Just "pending"

        Err _ ->
            False


caseDerivativeAbsent : Bool
caseDerivativeAbsent =
    case Decode.decodeString Post.detailDecoder """{"id":"43","preview_url":"/media/p","original_url":"/media/o","media_type":"image/png","width":4,"height":4,"source":"s","artist":"a","hash":"h","file_size":10,"created_at":"2026-01-01","tags":[]}""" of
        Ok detail ->
            detail.derivativeJobId == Nothing && detail.derivativeStatus == Nothing

        Err _ ->
            False


caseSummaryProjection : Bool
caseSummaryProjection =
    case Decode.decodeString Post.detailDecoder detailJson of
        Ok detail ->
            let
                projected =
                    Post.summaryOfDetail detail
            in
            projected.id == "42" && projected.previewUrl == "/media/p"

        Err _ ->
            False


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "jpeg accepted", caseJpegAccepted )
    , ( "png accepted", casePngAccepted )
    , ( "gif accepted", caseGifAccepted )
    , ( "unsupported mime rejected", caseBadMimeRejected )
    , ( "empty mime falls back to extension", caseEmptyMimeFallsBackToExtension )
    , ( "empty mime with unknown extension rejected", caseEmptyMimeUnknownExtensionRejected )
    , ( "oversized file rejected with limit message", caseOversizedRejected )
    , ( "empty file rejected", caseEmptyRejected )
    , ( "shared tags parsed and deduplicated", caseTagsParsed )
    , ( "byte sizes formatted", caseBytesFormatted )
    , ( "terminal job states detected", caseTerminalJobs )
    , ( "phase and thumbnail labels", casePhaseLabels )
    , ( "refresh replaces member previews in place", caseRefreshMemberInPlace )
    , ( "refresh never injects unknown ids", caseRefreshNeverInjects )
    , ( "dismissing an uploading entry is refused", caseDismissUploadingRefused )
    , ( "dismissing a saving entry is refused", caseDismissSavingRefused )
    , ( "dismissing a queued entry removes it", caseDismissQueuedRemoves )
    , ( "dismissing an unknown entry is a no-op", caseDismissUnknownNoop )
    , ( "dismissible phases", caseDismissiblePhases )
    , ( "derivative job fields decode", caseDerivativeDecodes )
    , ( "missing derivative fields decode as Nothing", caseDerivativeAbsent )
    , ( "detail projects to grid summary", caseSummaryProjection )
    , ( "locked queue holds queued entries", caseLockedPumpHoldsQueued )
    , ( "unlocked queue attempts queued entries", caseUnlockedPumpAttemptsQueued )
    , ( "resume while locked is a no-op", caseResumeLockedHolds )
    , ( "finish for a queued entry is ignored", caseFinishQueuedIgnored )
    , ( "lost upload response stays uncertain without retry", caseFinishTimeoutHonest )
    , ( "upload 401 holds later files until access changes", caseFinishUnauthorizedHoldsQueue )
    , ( "late upload 401 retains its request generation", caseStaleUnauthorizedCarriesRequestGeneration )
    , ( "confirmed upload stands through lock", caseFinishConfirmedStands )
    ]
