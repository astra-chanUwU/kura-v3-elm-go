module Feature.UploadQueue.Queue exposing
    ( Access
    , ChosenFiles
    , Effect(..)
    , Entry
    , Event(..)
    , FileInfo
    , JobReport
    , Model
    , Note(..)
    , Phase(..)
    , ThumbState(..)
    , UploadOutcome(..)
    , UploadedInfo
    , acceptMimes
    , activeUploadId
    , filesChosen
    , formatBytes
    , hasWaitingThumb
    , init
    , isDismissible
    , isDone
    , isOpen
    , maxBytes
    , maxPolls
    , nextId
    , parseTags
    , pendingCount
    , phaseLabel
    , resume
    , setOpen
    , thumbFailureMessage
    , thumbLabel
    , update
    , validateFile
    )

{-| Pure upload-queue state and transitions.

This module owns the queue state, file validation, and every queue
transition, but performs no effects itself: it never touches files,
credentials, HTTP, progress tracking, the picker, or timers. Browser and
HTTP happenings arrive as plain facts (`Event`), and every consequence
the coordinator must execute comes back as an explicit `Effect`
description. Page-directed notifications come back as `Note` values
that the coordinator maps to its own `OutMsg`.

Files picked by the user arrive as `FileInfo` facts; the coordinator
keeps the actual `File` values keyed by the entry ids this module
assigns and reports back from `filesChosen`. Presence of a stored file
is reported per call as a set of ids, so a file dropped outside the
queue fails its entry instead of stalling the queue behind it.

The phase path is Queued → Uploading → Saving → Uploaded. Uploads are
never retried automatically: after a lost response the post may already
exist, so a failed entry stays failed and only the remaining queue
pumps forward.
-}

import Domain.Post exposing (PostDetail)
import Domain.JobStatus as JobStatus
import Domain.UploadRules as UploadRules
import Set exposing (Set)


{-| Client-side bound mirroring the server: 25 MiB per file. The API
adapter and queue read this from one domain fact; the oversized-file
test pins the wording that carries the limit.
-}
maxBytes : Int
maxBytes =
    UploadRules.maxBytes


{-| Picker hint. Validation still runs on plain facts so files chosen by
other means get honest errors instead of silent acceptance.
-}
acceptMimes : List String
acceptMimes =
    [ "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "video/mp4", "video/webm" ]


{-| Thumbnail poll bound: 40 polls, after which the entry settles as a
thumbnail failure while staying Uploaded.
-}
maxPolls : Int
maxPolls =
    40


{-| Plain file facts. Validation runs on this record so the rules stay
unit-testable without browser file values.
-}
type alias FileInfo =
    { name : String
    , mime : String
    , size : Int
    }


type Phase
    = Queued
    | Uploading { sent : Int, size : Int, accessGeneration : Int }
    | Saving Int
    | Uploaded UploadedInfo
    | Rejected String
    | Failed String


type ThumbState
    = ThumbWaiting Int
    | ThumbReady
    | ThumbFailed String


type alias UploadedInfo =
    { postId : String
    , jobId : Maybe String
    , thumb : ThumbState
    }


type alias Entry =
    { id : Int
    , name : String
    , size : Int
    , mime : String
    , tags : List String
    , source : String
    , artist : String
    , phase : Phase
    }


type alias Model =
    { open : Bool
    , tagsDraft : String
    , sourceDraft : String
    , artistDraft : String
    , entries : List Entry
    , nextId : Int
    }


{-| Per-call access facts: whether writes are currently enabled and the
current credential generation. The queue never stores the credential.
-}
type alias Access =
    { writesEnabled : Bool
    , accessGeneration : Int
    }


{-| Plain facts translated from browser and HTTP events by the
coordinator. No `File`, `Http`, or `Time` values cross this boundary.
-}
type Event
    = PanelOpened
    | PanelClosed
    | TagsEdited String
    | SourceEdited String
    | ArtistEdited String
    | ProgressTick { entryId : Int, sent : Int, size : Int, isSending : Bool }
    | UploadSettled { entryId : Int, generation : Int, outcome : UploadOutcome }
    | PollTick
    | JobPolled { entryId : Int, report : Maybe JobReport }
    | DismissRequested Int
    | FinishedCleared


{-| A settled upload: either the confirmed detail or a failure carrying
its display message plus whether the server rejected the credential.
-}
type UploadOutcome
    = CompletedUpload PostDetail
    | FailedUpload { message : String, unauthorized : Bool }


{-| A thumbnail poll report: the terminal outcome and display facts. A
missing report means the poll itself failed transiently and the entry
keeps waiting within the poll bound.
-}
type alias JobReport =
    { succeeded : Bool
    , terminal : Bool
    , status : String
    , lastError : String
    }


{-| Effect descriptions for the coordinator to execute. `BeginUpload`
and `FetchJob` name entries whose stored values the coordinator holds;
`DropFile`/`DropFiles` name stored values the coordinator must release.
-}
type Effect
    = PickFiles
    | BeginUpload Int
    | FetchJob Int String
    | DropFile Int
    | DropFiles (List Int)


{-| Page-directed notifications. The coordinator maps these to its own
out messages unchanged.
-}
type Note
    = UploadConfirmed PostDetail
    | ThumbCompleted String
    | CredentialRejected Int


init : Model
init =
    { open = False
    , tagsDraft = ""
    , sourceDraft = ""
    , artistDraft = ""
    , entries = []
    , nextId = 0
    }


isOpen : Model -> Bool
isOpen model =
    model.open


setOpen : Bool -> Model -> Model
setOpen open model =
    { model | open = open }


nextId : Model -> Int
nextId model =
    model.nextId


{-| Only settled or never-started entries may be dismissed. Active
`Uploading`/`Saving` entries keep their request and tracker until the
response settles; the transition below enforces this even if a dismiss
arrives from anywhere else.
-}
isDismissible : Entry -> Bool
isDismissible entry =
    case entry.phase of
        Uploading _ ->
            False

        Saving _ ->
            False

        _ ->
            True


isActivePhase : Phase -> Bool
isActivePhase phase =
    case phase of
        Uploading _ ->
            True

        Saving _ ->
            True

        _ ->
            False


activeUploadId : Model -> Maybe Int
activeUploadId model =
    List.filter (\entry -> isActivePhase entry.phase) model.entries
        |> List.head
        |> Maybe.map .id


hasWaitingThumb : Model -> Bool
hasWaitingThumb model =
    List.any isWaitingThumb model.entries


isWaitingThumb : Entry -> Bool
isWaitingThumb entry =
    case entry.phase of
        Uploaded info ->
            case info.thumb of
                ThumbWaiting _ ->
                    True

                _ ->
                    False

        _ ->
            False


isDoneEntry : Entry -> Bool
isDoneEntry entry =
    case entry.phase of
        Uploaded info ->
            case info.thumb of
                ThumbWaiting _ ->
                    False

                _ ->
                    True

        Rejected _ ->
            True

        Failed _ ->
            True

        _ ->
            False


isDone : Entry -> Bool
isDone =
    isDoneEntry


pendingCount : Model -> Int
pendingCount model =
    List.filter (not << isDoneEntry) model.entries |> List.length


{-| Shared-tags field: comma-separated, trimmed, empties dropped,
duplicates removed in order.
-}
parseTags : String -> List String
parseTags raw =
    List.foldl
        (\part ( seen, kept ) ->
            let
                tag =
                    String.trim part
            in
            if tag == "" || List.member tag seen then
                ( seen, kept )

            else
                ( tag :: seen, kept ++ [ tag ] )
        )
        ( [], [] )
        (String.split "," raw)
        |> Tuple.second


allowedExtensions : List String
allowedExtensions =
    [ ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".mp4", ".webm" ]


{-| Accept JPEG, PNG, GIF, WebP, AVIF, MP4, and WebM up to 25 MiB. An empty
MIME type falls back to the file extension; anything else is rejected with a
useful message. The server verifies the actual bytes and codecs.
-}
validateFile : FileInfo -> Result String ()
validateFile info =
    if String.trim info.name == "" then
        Err "The file has no name and cannot be uploaded."

    else if info.size <= 0 then
        Err "The file is empty."

    else if info.size > maxBytes then
        Err ("The file is " ++ formatBytes info.size ++ "; uploads are limited to 25 MiB each.")

    else
        let
            mime =
                String.toLower (String.trim info.mime)

            extension =
                String.toLower info.name
        in
        if mime == "" then
            if List.any (\ext -> String.endsWith ext extension) allowedExtensions then
                Ok ()

            else
                Err "The file is not a supported image or video. Upload JPEG, PNG, GIF, WebP, AVIF, MP4, or WebM."

        else if List.member mime acceptMimes then
            Ok ()

        else
            Err ("Files of type " ++ info.mime ++ " are not supported. Upload JPEG, PNG, GIF, WebP, AVIF, MP4, or WebM.")


formatBytes : Int -> String
formatBytes bytes =
    if bytes < 1024 then
        String.fromInt bytes ++ " B"

    else if bytes < 1024 * 1024 then
        oneDecimal (toFloat bytes / 1024) ++ " KiB"

    else
        oneDecimal (toFloat bytes / (1024 * 1024)) ++ " MiB"


oneDecimal : Float -> String
oneDecimal value =
    let
        rounded =
            toFloat (round (value * 10)) / 10

        whole =
            floor rounded

        tenth =
            round ((rounded - toFloat whole) * 10)
    in
    String.fromInt whole ++ "." ++ String.fromInt tenth


phaseLabel : Phase -> String
phaseLabel phase =
    case phase of
        Queued ->
            "Queued"

        Uploading _ ->
            "Uploading"

        Saving _ ->
            "Saving"

        Uploaded _ ->
            "Uploaded"

        Rejected _ ->
            "Rejected"

        Failed _ ->
            "Failed"


thumbLabel : ThumbState -> String
thumbLabel thumb =
    case thumb of
        ThumbWaiting _ ->
            "Thumbnail: processing…"

        ThumbReady ->
            "Thumbnail: ready"

        ThumbFailed _ ->
            "Thumbnail: failed — the original was saved"


{-| Thumbnail failure wording for a terminal job state. The original was
saved, so the entry stays uploaded either way.
-}
thumbFailureMessage : String -> String -> String
thumbFailureMessage status lastError =
    if String.trim lastError /= "" then
        "Thumbnail failed: " ++ lastError ++ " — the original was saved."

    else
        "Thumbnail " ++ status ++ " — the original was saved."


{-| The settled result of newly picked files: the transitioned model,
the assigned entry ids (so the coordinator can store the matching
browser values under the same ids), and the effects and notes to run.
-}
type alias ChosenFiles =
    { model : Model
    , assigned : List Int
    , effects : List Effect
    , notes : List Note
    }


{-| New files arrive as validated facts. Entry ids assign sequentially
from `nextId`. A rejected file never starts and never blocks the rest
of the queue.
-}
filesChosen : Access -> Set Int -> List FileInfo -> Model -> ChosenFiles
filesChosen access stored infos model =
    let
        sharedTags =
            parseTags model.tagsDraft

        sharedSource =
            String.trim model.sourceDraft

        sharedArtist =
            String.trim model.artistDraft

        toEntry info entryId =
            { id = entryId
            , name = info.name
            , size = info.size
            , mime = info.mime
            , tags = sharedTags
            , source = sharedSource
            , artist = sharedArtist
            , phase =
                case validateFile info of
                    Ok () ->
                        Queued

                    Err message ->
                        Rejected message
            }

        entries =
            List.indexedMap (\offset info -> toEntry info (model.nextId + offset)) infos

        assigned =
            List.map .id entries

        ( pumped, effects, notes ) =
            pump access (Set.union stored (Set.fromList assigned)) { model | entries = model.entries ++ entries, nextId = model.nextId + List.length infos }
    in
    { model = pumped, assigned = assigned, effects = effects, notes = notes }


{-| Resume a paused queue after unlock. Queued entries start only while
writes are enabled; while locked this is a no-op so a lock can never
launch a new upload. In-flight entries are untouched either way.
-}
resume : Access -> Set Int -> Model -> ( Model, List Effect, List Note )
resume access stored model =
    if access.writesEnabled then
        pump access stored model

    else
        ( model, [], [] )


update : Access -> Set Int -> Event -> Model -> ( Model, List Effect, List Note )
update access stored event model =
    case event of
        PanelOpened ->
            ( { model | open = True }, [], [] )

        PanelClosed ->
            ( { model | open = False }, [], [] )

        TagsEdited value ->
            ( { model | tagsDraft = value }, [], [] )

        SourceEdited value ->
            ( { model | sourceDraft = value }, [], [] )

        ArtistEdited value ->
            ( { model | artistDraft = value }, [], [] )

        ProgressTick tick ->
            ( { model | entries = List.map (applyProgress tick) model.entries }, [], [] )

        UploadSettled settled ->
            finishUpload access stored model settled.entryId settled.generation settled.outcome

        PollTick ->
            pollThumbs model

        JobPolled polled ->
            jobSettled model polled.entryId polled.report

        DismissRequested entryId ->
            case List.filter (\entry -> entry.id == entryId) model.entries |> List.head of
                Just entry ->
                    if isActivePhase entry.phase then
                        -- Refused here, not just in the view: the request
                        -- and its tracker stay alive until the response
                        -- settles.
                        ( model, [], [] )

                    else
                        let
                            ( pumped, effects, notes ) =
                                pump access stored
                                    { model | entries = List.filter (\candidate -> candidate.id /= entryId) model.entries }
                        in
                        ( pumped, DropFile entryId :: effects, notes )

                Nothing ->
                    ( model, [], [] )

        FinishedCleared ->
            let
                kept =
                    List.filter (not << isDoneEntry) model.entries

                keptIds =
                    List.map .id kept

                dropped =
                    List.map .id model.entries
                        |> List.filter (\id -> not (List.member id keptIds))
            in
            ( { model | entries = kept }
            , if List.isEmpty dropped then
                []

              else
                [ DropFiles dropped ]
            , []
            )


{-| Start the first queued entry when nothing is in flight and writes are
enabled. While locked the queue holds its place: queued entries wait for
`resume` instead of launching uncredentialed uploads that a gated server
would reject.
-}
pump : Access -> Set Int -> Model -> ( Model, List Effect, List Note )
pump access stored model =
    startNext access stored model


startNext : Access -> Set Int -> Model -> ( Model, List Effect, List Note )
startNext access stored model =
    if not access.writesEnabled then
        ( model, [], [] )

    else
        case ( activeEntry model, List.filter (\entry -> entry.phase == Queued) model.entries |> List.head ) of
            ( Nothing, Just next ) ->
                if Set.member next.id stored then
                    ( { model | entries = List.map (beginUpload next.id access.accessGeneration) model.entries }
                    , [ BeginUpload next.id ]
                    , []
                    )

                else
                    -- The picked value is gone; fail this entry and move on
                    -- rather than stalling the queue behind it.
                    startNext access stored
                        { model
                            | entries =
                                List.map
                                    (\entry ->
                                        if entry.id == next.id then
                                            { entry | phase = Failed "The selected file is no longer available." }

                                        else
                                            entry
                                    )
                                    model.entries
                        }

            _ ->
                ( model, [], [] )


beginUpload : Int -> Int -> Entry -> Entry
beginUpload entryId accessGeneration entry =
    if entry.id == entryId && entry.phase == Queued then
        { entry
            | phase =
                Uploading
                    { sent = 0
                    , size = max 1 entry.size
                    , accessGeneration = accessGeneration
                    }
        }

    else
        entry


activeEntry : Model -> Maybe Entry
activeEntry model =
    List.filter (\entry -> isActivePhase entry.phase) model.entries
        |> List.head


applyProgress : { entryId : Int, sent : Int, size : Int, isSending : Bool } -> Entry -> Entry
applyProgress tick entry =
    if entry.id /= tick.entryId then
        entry

    else if tick.isSending then
        case entry.phase of
            Uploading current ->
                if tick.sent >= tick.size then
                    { entry | phase = Saving current.accessGeneration }

                else
                    { entry
                        | phase =
                            Uploading
                                { sent = tick.sent
                                , size = tick.size
                                , accessGeneration = current.accessGeneration
                                }
                    }

            Saving accessGeneration ->
                if tick.sent < tick.size then
                    { entry
                        | phase =
                            Uploading
                                { sent = tick.sent
                                , size = tick.size
                                , accessGeneration = accessGeneration
                                }
                    }

                else
                    entry

            _ ->
                entry

    else
        case entry.phase of
            Uploading current ->
                { entry | phase = Saving current.accessGeneration }

            _ ->
                entry


finishUpload : Access -> Set Int -> Model -> Int -> Int -> UploadOutcome -> ( Model, List Effect, List Note )
finishUpload access stored model entryId requestGeneration outcome =
    case List.filter (\entry -> entry.id == entryId) model.entries |> List.head of
        Nothing ->
            ( model, [], [] )

        Just entry ->
            case entry.phase of
                Uploading active ->
                    if active.accessGeneration == requestGeneration then
                        settleUpload access stored model entry outcome

                    else
                        ( model, [], [] )

                Saving activeGeneration ->
                    if activeGeneration == requestGeneration then
                        settleUpload access stored model entry outcome

                    else
                        ( model, [], [] )

                _ ->
                    ( model, [], [] )


{-| Settle one upload honestly. A confirmed upload stands even when the
browser locked mid-flight: lock is not rollback, and the entry keeps
its server-confirmed post id. Failures never resend: after a lost
response the post may already exist, so resubmission stays an explicit
user decision and only the remaining queue pumps forward.
-}
settleUpload : Access -> Set Int -> Model -> Entry -> UploadOutcome -> ( Model, List Effect, List Note )
settleUpload access stored model entry outcome =
    case outcome of
        CompletedUpload detail ->
            let
                uploaded =
                    { entry
                        | phase =
                            Uploaded
                                { postId = detail.id
                                , jobId = detail.derivativeJobId
                                , thumb = initialThumb detail.derivativeJobId detail.derivativeStatus
                                }
                    }

                withSettled =
                    { model
                        | entries = List.map (\candidate -> if candidate.id == entry.id then uploaded else candidate) model.entries
                    }

                ( pumped, effects, _ ) =
                    pump access stored withSettled
            in
            ( pumped, DropFile entry.id :: effects, [ UploadConfirmed detail ] )

        FailedUpload failed ->
            let
                withFailed =
                    { model
                        | entries =
                            List.map
                                (\candidate ->
                                    if candidate.id == entry.id then
                                        { entry | phase = Failed failed.message }

                                    else
                                        candidate
                                )
                                model.entries
                    }

                ( pumped, effects, _ ) =
                    if failed.unauthorized && access.accessGeneration /= entryGeneration entry && access.writesEnabled then
                        pump access stored withFailed

                    else if not failed.unauthorized then
                        pump access stored withFailed

                    else
                        ( withFailed, [], [] )

                notes =
                    if failed.unauthorized then
                        [ CredentialRejected (entryGeneration entry) ]

                    else
                        []
            in
            ( pumped, DropFile entry.id :: effects, notes )


entryGeneration : Entry -> Int
entryGeneration entry =
    case entry.phase of
        Uploading active ->
            active.accessGeneration

        Saving generation ->
            generation

        _ ->
            -1


{-| The thumbnail starts from the confirmed derivative facts: no job
means ready now, a terminal state settles immediately, and anything
else waits within the poll bound.
-}
initialThumb : Maybe String -> Maybe String -> ThumbState
initialThumb jobId status =
    case jobId of
        Nothing ->
            ThumbReady

        Just _ ->
            case status of
                Just current ->
                    if isTerminalStatus current then
                        if current == "succeeded" then
                            ThumbReady

                        else
                            ThumbFailed ("Thumbnail " ++ current ++ " — the original was saved.")

                    else
                        ThumbWaiting 0

                Nothing ->
                    ThumbWaiting 0


{-| Mirrors the backend job lifecycle: only succeeded, failed, and
cancelled stop the poll.
-}
isTerminalStatus : String -> Bool
isTerminalStatus status =
    JobStatus.isTerminal status


pollThumbs : Model -> ( Model, List Effect, List Note )
pollThumbs model =
    let
        ( entries, effects ) =
            List.foldr
                (\entry ( kept, polls ) ->
                    case entry.phase of
                        Uploaded info ->
                            case info.thumb of
                                ThumbWaiting count ->
                                    if count >= maxPolls then
                                        ( { entry | phase = Uploaded { info | thumb = ThumbFailed "Thumbnail status timed out. The original was saved." } } :: kept
                                        , polls
                                        )

                                    else
                                        case info.jobId of
                                            Just jobId ->
                                                ( { entry | phase = Uploaded { info | thumb = ThumbWaiting (count + 1) } } :: kept
                                                , FetchJob entry.id jobId :: polls
                                                )

                                            Nothing ->
                                                ( { entry | phase = Uploaded { info | thumb = ThumbReady } } :: kept
                                                , polls
                                                )

                                _ ->
                                    ( entry :: kept, polls )

                        _ ->
                            ( entry :: kept, polls )
                )
                ( [], [] )
                model.entries
    in
    ( { model | entries = entries }, effects, [] )


jobSettled : Model -> Int -> Maybe JobReport -> ( Model, List Effect, List Note )
jobSettled model entryId report =
    let
        entries =
            List.map (applyJobResult entryId report) model.entries

        notes =
            case report of
                Just current ->
                    if current.succeeded then
                        case List.filter (\entry -> entry.id == entryId) model.entries |> List.head of
                            Just entry ->
                                case entry.phase of
                                    Uploaded info ->
                                        case info.thumb of
                                            ThumbWaiting _ ->
                                                [ ThumbCompleted info.postId ]

                                            _ ->
                                                []

                                    _ ->
                                        []

                            Nothing ->
                                []

                    else
                        []

                Nothing ->
                    []
    in
    ( { model | entries = entries }, [], notes )


applyJobResult : Int -> Maybe JobReport -> Entry -> Entry
applyJobResult entryId report entry =
    if entry.id /= entryId then
        entry

    else
        case ( entry.phase, report ) of
            ( Uploaded info, Just current ) ->
                case info.thumb of
                    ThumbWaiting _ ->
                        if current.terminal then
                            if current.succeeded then
                                { entry | phase = Uploaded { info | thumb = ThumbReady } }

                            else
                                { entry
                                    | phase =
                                        Uploaded
                                            { info | thumb = ThumbFailed (thumbFailureMessage current.status current.lastError) }
                                }

                        else
                            entry

                    _ ->
                        entry

            _ ->
                entry
