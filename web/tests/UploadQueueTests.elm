module UploadQueueTests exposing (allPassed, suite)

import Api.Job as Job
import Domain.Post as Post
import Domain.Sequence as Sequence
import Feature.UploadQueue as UploadQueue exposing (Entry, FileInfo, Model, Msg(..))
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
        && UploadQueue.phaseLabel UploadQueue.Saving == "Saving"
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


dismissEntries : Model -> Int -> List Entry
dismissEntries model entryId =
    UploadQueue.update "" (DismissEntry entryId) model
        |> (\( next, _, _ ) -> next.entries)


caseDismissUploadingRefused : Bool
caseDismissUploadingRefused =
    let
        model =
            testModel [ testEntry 0 (UploadQueue.Uploading { sent = 10, size = 100 }) ]
    in
    dismissEntries model 0 == model.entries


caseDismissSavingRefused : Bool
caseDismissSavingRefused =
    let
        model =
            testModel [ testEntry 0 UploadQueue.Saving, testEntry 1 UploadQueue.Queued ]
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
        && not (UploadQueue.isDismissible (testEntry 0 (UploadQueue.Uploading { sent = 0, size = 1 })))
        && not (UploadQueue.isDismissible (testEntry 0 UploadQueue.Saving))


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
    ]
