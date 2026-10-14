module QueueTests exposing (allPassed, suite)

import Domain.Rating
import Domain.JobStatus as JobStatus
import Feature.UploadQueue.Queue as Queue exposing (Effect(..), Event(..), Note(..))
import Set


access : Queue.Access
access =
    { writesEnabled = True, accessGeneration = 7 }


locked : Queue.Access
locked =
    { writesEnabled = False, accessGeneration = 7 }


info : String -> Queue.FileInfo
info name =
    { name = name, mime = "image/jpeg", size = 1024 }


stored : List Int -> Set.Set Int
stored =
    Set.fromList


detail jobId status =
    { id = "p1"
    , previewUrl = "/m"
    , originalUrl = "/o"
    , mediaType = "image/jpeg"
    , width = 1
    , height = 1
    , source = ""
    , artist = ""
    , hash = "h"
    , fileSize = 1
    , createdAt = "2026-01-01"
    , tags = []
    , tagVersion = 1
    , favorite = False
    , score = 0
    , reactionVersion = 1
    , history = []
    , reactionHistory = []
    , derivativeJobId = jobId
    , derivativeStatus = status
    , metadataVersion = 0
    , metadataHistory = []
    , rating = Domain.Rating.Unrated
    , ratingVersion = Just 0
    , ratingHistory = []
    }



phasesOf : Queue.Model -> List String
phasesOf model =
    List.map (\entry -> Queue.phaseLabel entry.phase) model.entries


picked : Queue.Model
picked =
    Queue.filesChosen access (stored [ 0 ]) [ info "a.jpg" ] Queue.init
        |> .model


uploading : Queue.Model
uploading =
    picked


settleUpload : Queue.UploadOutcome -> Queue.Model -> ( Queue.Model, List Effect, List Note )
settleUpload outcome model =
    Queue.update access (stored [ 0, 1 ]) (UploadSettled { entryId = 0, generation = 7, outcome = outcome }) model


casePickStartsFirst : Bool
casePickStartsFirst =
    let
        chosen =
            Queue.filesChosen access (stored []) [ info "a.jpg", info "b.jpg" ] Queue.init
    in
    chosen.assigned == [ 0, 1 ]
        && chosen.effects == [ BeginUpload 0 ]
        && phasesOf chosen.model == [ "Uploading", "Queued" ]


casePickWhileLockedHolds : Bool
casePickWhileLockedHolds =
    let
        chosen =
            Queue.filesChosen locked (stored []) [ info "a.jpg" ] Queue.init
    in
    chosen.assigned == [ 0 ]
        && chosen.effects == []
        && phasesOf chosen.model == [ "Queued" ]


casePickRejectsWithoutBlocking : Bool
casePickRejectsWithoutBlocking =
    let
        bad =
            { name = "notes.txt", mime = "text/plain", size = 10 }

        chosen =
            Queue.filesChosen access (stored []) [ bad, { name = "a.jpg", mime = "image/jpeg", size = 10 } ] Queue.init
    in
    chosen.effects == [ BeginUpload 1 ]
        && phasesOf chosen.model == [ "Rejected", "Uploading" ]


caseMissingFileFailsForward : Bool
caseMissingFileFailsForward =
    let
        -- Picked while locked: queued entries with no stored values yet.
        held =
            Queue.filesChosen locked (stored []) [ info "a.jpg", info "b.jpg" ] Queue.init
                |> .model

        ( settled, effects, _ ) =
            Queue.update access (stored []) (DismissRequested 1) held
    in
    -- The dismissed entry leaves, and the missing value fails its entry
    -- instead of stalling the queue behind it.
    effects == [ DropFile 1 ] && phasesOf settled == [ "Failed" ]


caseUnknownDismissNoop : Bool
caseUnknownDismissNoop =
    let
        ( next, effects, _ ) =
            Queue.update access (stored [ 0, 1 ]) (DismissRequested 99) uploading
    in
    -- Unknown dismiss changes nothing and starts nothing new.
    effects == [] && next == uploading


caseSuccessConfirmsAndPumps : Bool
caseSuccessConfirmsAndPumps =
    let
        two =
            Queue.filesChosen access (stored [ 0, 1 ]) [ info "a.jpg", info "b.jpg" ] Queue.init
                |> .model

        ( settledModel, effects, notes ) =
            Queue.update access (stored [ 0, 1 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail Nothing Nothing) }) two

        phases =
            phasesOf settledModel
    in
    notes == [ UploadConfirmed (detail Nothing Nothing) ]
        && phases == [ "Uploaded", "Uploading" ]
        && List.member (BeginUpload 1) effects
        && List.member (DropFile 0) effects


caseFailureNeverRetriesItself : Bool
caseFailureNeverRetriesItself =
    let
        two =
            Queue.filesChosen access (stored [ 0, 1 ]) [ info "a.jpg", info "b.jpg" ] Queue.init
                |> .model

        failed =
            Queue.FailedUpload { message = "boom", unauthorized = False }

        ( settledModel, effects, notes ) =
            Queue.update access (stored [ 0, 1 ]) (UploadSettled { entryId = 0, generation = 7, outcome = failed }) two
    in
    notes == []
        && phasesOf settledModel == [ "Failed", "Uploading" ]
        && not (List.member (BeginUpload 0) effects)
        && List.member (BeginUpload 1) effects


caseUnauthorizedStaleStillPumps : Bool
caseUnauthorizedStaleStillPumps =
    let
        staleAccess =
            { writesEnabled = True, accessGeneration = 8 }

        two =
            Queue.filesChosen access (stored [ 0, 1 ]) [ info "a.jpg", info "b.jpg" ] Queue.init
                |> .model

        failed =
            Queue.FailedUpload { message = "denied", unauthorized = True }

        ( settledModel, effects, notes ) =
            Queue.update staleAccess (stored [ 0, 1 ]) (UploadSettled { entryId = 0, generation = 7, outcome = failed }) two
    in
    -- The epoch moved on after the request: the entry still settles
    -- honestly and reports, and the remaining queue pumps forward.
    notes == [ CredentialRejected 7 ]
        && List.member (DropFile 0) effects
        && List.member (BeginUpload 1) effects
        && phasesOf settledModel == [ "Failed", "Uploading" ]


caseUnauthorizedCurrentHolds : Bool
caseUnauthorizedCurrentHolds =
    let
        failed =
            Queue.FailedUpload { message = "denied", unauthorized = True }

        ( settledModel, effects, notes ) =
            settleUpload failed uploading
    in
    -- Same epoch: the queue halts for the lock instead of pumping.
    notes == [ CredentialRejected 7 ]
        && effects == [ DropFile 0 ]
        && phasesOf settledModel == [ "Failed" ]


caseWrongGenerationIgnored : Bool
caseWrongGenerationIgnored =
    let
        failed =
            Queue.FailedUpload { message = "denied", unauthorized = True }

        ( settledModel, effects, notes ) =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 3, outcome = Queue.CompletedUpload (detail Nothing Nothing) }) uploading
    in
    notes == [] && effects == [] && settledModel == uploading


caseProgressCompletesToSaving : Bool
caseProgressCompletesToSaving =
    let
        tick =
            ProgressTick { entryId = 0, sent = 100, size = 100, isSending = True }

        ( next, effects, notes ) =
            Queue.update access (stored [ 0 ]) tick uploading
    in
    effects == [] && notes == [] && phasesOf next == [ "Saving" ]


caseProgressPartialStaysUploading : Bool
caseProgressPartialStaysUploading =
    let
        tick =
            ProgressTick { entryId = 0, sent = 10, size = 100, isSending = True }

        ( next, _, _ ) =
            Queue.update access (stored [ 0 ]) tick uploading
    in
    phasesOf next == [ "Uploading" ]


caseReceivingMovesToSaving : Bool
caseReceivingMovesToSaving =
    let
        tick =
            ProgressTick { entryId = 0, sent = 0, size = 0, isSending = False }

        ( next, _, _ ) =
            Queue.update access (stored [ 0 ]) tick uploading
    in
    phasesOf next == [ "Saving" ]


casePollEmitsBoundedFetches : Bool
casePollEmitsBoundedFetches =
    let
        waiting =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail (Just "job-1") (Just "running")) }) uploading
                |> (\( m, _, _ ) -> m)

        ( _, effects, _ ) =
            Queue.update access (stored [ 0 ]) PollTick waiting
    in
    effects == [ FetchJob 0 "job-1" ]


casePollCapSettlesFailure : Bool
casePollCapSettlesFailure =
    let
        waiting =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail (Just "job-1") (Just "running")) }) uploading
                |> (\( m, _, _ ) -> m)

        ticked =
            List.range 1 Queue.maxPolls
                |> List.foldl (\_ m -> Queue.update access (stored [ 0 ]) PollTick m |> (\( n, _, _ ) -> n)) waiting

        ( settled, effects, _ ) =
            Queue.update access (stored [ 0 ]) PollTick ticked
    in
    effects == []
        && List.filterMap
            (\entry ->
                case entry.phase of
                    Queue.Uploaded uploadedInfo ->
                        Just uploadedInfo.thumb

                    _ ->
                        Nothing
            )
            settled.entries
            == [ Queue.ThumbFailed "Thumbnail status timed out. The original was saved." ]


caseJobSuccessCompletesThumb : Bool
caseJobSuccessCompletesThumb =
    let
        waiting =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail (Just "job-1") (Just "running")) }) uploading
                |> (\( m, _, _ ) -> m)

        report =
            Just { succeeded = True, terminal = True, status = "succeeded", lastError = "" }

        ( _, _, notes ) =
            Queue.update access (stored [ 0 ]) (JobPolled { entryId = 0, report = report }) waiting
    in
    notes == [ ThumbCompleted "p1" ]


caseJobSuccessAfterReadySilent : Bool
caseJobSuccessAfterReadySilent =
    let
        ready =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail Nothing Nothing) }) uploading
                |> (\( m, _, _ ) -> m)

        report =
            Just { succeeded = True, terminal = True, status = "succeeded", lastError = "" }

        ( _, _, notes ) =
            Queue.update access (stored [ 0 ]) (JobPolled { entryId = 0, report = report }) ready
    in
    notes == []


caseJobFailureKeepsUpload : Bool
caseJobFailureKeepsUpload =
    let
        waiting =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail (Just "job-1") (Just "running")) }) uploading
                |> (\( m, _, _ ) -> m)

        report =
            Just { succeeded = False, terminal = True, status = "failed", lastError = "bad frame" }

        ( settled, _, notes ) =
            Queue.update access (stored [ 0 ]) (JobPolled { entryId = 0, report = report }) waiting

        thumbs =
            List.filterMap
                (\entry ->
                    case entry.phase of
                        Queue.Uploaded uploadedInfo ->
                            Just uploadedInfo.thumb

                        _ ->
                            Nothing
                )
                settled.entries
    in
    notes == [] && thumbs == [ Queue.ThumbFailed "Thumbnail failed: bad frame — the original was saved." ]


caseTransientPollKeepsWaiting : Bool
caseTransientPollKeepsWaiting =
    let
        waiting =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail (Just "job-1") (Just "running")) }) uploading
                |> (\( m, _, _ ) -> m)

        ( settled, _, notes ) =
            Queue.update access (stored [ 0 ]) (JobPolled { entryId = 0, report = Nothing }) waiting
    in
    notes == [] && settled == waiting


caseDismissActiveRefused : Bool
caseDismissActiveRefused =
    let
        ( settled, effects, notes ) =
            Queue.update access (stored [ 0 ]) (DismissRequested 0) uploading
    in
    settled == uploading && effects == [] && notes == []


caseClearFinishedDropsStored : Bool
caseClearFinishedDropsStored =
    let
        done =
            Queue.update access (stored [ 0 ]) (UploadSettled { entryId = 0, generation = 7, outcome = Queue.CompletedUpload (detail Nothing Nothing) }) uploading
                |> (\( m, _, _ ) -> m)

        ( cleared, effects, _ ) =
            Queue.update access (stored [ 0 ]) FinishedCleared done
    in
    cleared.entries == [] && effects == [ DropFiles [ 0 ] ]


caseTerminalMirror : Bool
caseTerminalMirror =
    Queue.thumbFailureMessage "failed" "" == "Thumbnail failed — the original was saved."
        && Queue.thumbFailureMessage "cancelled" "  " == "Thumbnail cancelled — the original was saved."


caseSharedTerminalStatuses : Bool
caseSharedTerminalStatuses =
    JobStatus.isTerminal "succeeded"
        && JobStatus.isTerminal "failed"
        && JobStatus.isTerminal "cancelled"
        && not (JobStatus.isTerminal "pending")
        && not (JobStatus.isTerminal "running")
        && not (JobStatus.isTerminal "")


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "pick starts the first entry", casePickStartsFirst )
    , ( "pick while locked holds", casePickWhileLockedHolds )
    , ( "pick rejects without blocking", casePickRejectsWithoutBlocking )
    , ( "missing stored values fail forward", caseMissingFileFailsForward )
    , ( "unknown dismiss changes nothing", caseUnknownDismissNoop )
    , ( "success confirms and pumps", caseSuccessConfirmsAndPumps )
    , ( "failure never retries itself", caseFailureNeverRetriesItself )
    , ( "stale unauthorized still settles", caseUnauthorizedStaleStillPumps )
    , ( "current unauthorized reports once", caseUnauthorizedCurrentHolds )
    , ( "wrong generation ignored", caseWrongGenerationIgnored )
    , ( "progress completes to saving", caseProgressCompletesToSaving )
    , ( "partial progress stays uploading", caseProgressPartialStaysUploading )
    , ( "receiving moves to saving", caseReceivingMovesToSaving )
    , ( "poll emits bounded fetches", casePollEmitsBoundedFetches )
    , ( "poll cap settles failure", casePollCapSettlesFailure )
    , ( "job success completes the thumb", caseJobSuccessCompletesThumb )
    , ( "late job success stays silent", caseJobSuccessAfterReadySilent )
    , ( "job failure keeps the upload", caseJobFailureKeepsUpload )
    , ( "transient poll keeps waiting", caseTransientPollKeepsWaiting )
    , ( "dismiss active refused", caseDismissActiveRefused )
    , ( "clear finished drops stored", caseClearFinishedDropsStored )
    , ( "terminal wording mirror", caseTerminalMirror )
    , ( "shared terminal job status facts", caseSharedTerminalStatuses )
    ]
