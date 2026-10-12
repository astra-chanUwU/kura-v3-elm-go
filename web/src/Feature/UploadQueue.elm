module Feature.UploadQueue exposing
    ( Context
    , Entry
    , FileInfo
    , Model
    , Msg(..)
    , OutMsg(..)
    , acceptMimes
    , activeUploadId
    , context
    , formatBytes
    , init
    , isDismissible
    , isDone
    , isOpen
    , maxPolls
    , parseTags
    , pendingCount
    , phaseLabel
    , pollIntervalMs
    , resume
    , setOpen
    , subscriptions
    , thumbLabel
    , trackerKey
    , update
    , validateFile
    , view
    )

{-| Small sequential upload queue: the page-facing coordinator.

Browser and HTTP effects live here: picked `File` values, credentials,
upload and job requests, transfer-progress tracking, the file picker,
and thumbnail polling. Pure queue state, validation, and transitions
live in `Feature.UploadQueue.Queue`, which receives plain facts and
returns explicit effect descriptions; rendering lives in
`Feature.UploadQueue.View`. The phase path is Queued → Uploading →
Saving → Uploaded, uploads are never retried automatically, and a
thumbnail failure keeps a confirmed upload saved.
-}

import Api.Job as Job
import Api.Upload as Upload
import Dict exposing (Dict)
import Domain.Post exposing (PostDetail)
import Feature.UploadQueue.Queue as Queue
import Feature.UploadQueue.View as UploadView
import File exposing (File)
import File.Select as Select
import Html exposing (Html)
import Http
import Set exposing (Set)
import Time


{-| Queue entries hold metadata and phase; the picked `File` values live
in `files` keyed by entry id. Separating the two keeps dismissal able
to drop a file without touching the phase of any other entry.
-}
type alias Model =
    { open : Bool
    , tagsDraft : String
    , sourceDraft : String
    , artistDraft : String
    , entries : List Entry
    , files : Dict Int File
    , nextId : Int
    }


type alias Entry =
    Queue.Entry


type alias FileInfo =
    Queue.FileInfo


{-| Per-call access context from the Library: where to send uploads,
which credential to attach, and whether writes are currently enabled.
The queue never stores the credential; every request is built from the
context handed to that update.
-}
type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    }


type Msg
    = OpenPanel
    | ClosePanel
    | TagsChanged String
    | SourceChanged String
    | ArtistChanged String
    | BrowseClicked
    | FilesPicked File (List File)
    | TransferProgress Int Http.Progress
    | UploadFinished Int Int (Result Http.Error PostDetail)
    | JobTick Time.Posix
    | JobFinished Int (Result Http.Error Job.Job)
    | ViewPost String
    | DismissEntry Int
    | ClearFinished


type OutMsg
    = UploadConfirmed PostDetail
    | ThumbCompleted String
    | OpenPost String
    | CredentialRejected Int


init : Model
init =
    { open = False
    , tagsDraft = ""
    , sourceDraft = ""
    , artistDraft = ""
    , entries = []
    , files = Dict.empty
    , nextId = 0
    }


{-| Picker hint. Validation still runs on plain facts so files chosen by
other means get honest errors instead of silent acceptance.
-}
acceptMimes : List String
acceptMimes =
    Queue.acceptMimes


{-| Thumbnail poll cadence: 1.5s ticks while an entry waits.
-}
pollIntervalMs : Float
pollIntervalMs =
    1500


{-| Thumbnail poll bound, mirrored from the pure transitions.
-}
maxPolls : Int
maxPolls =
    Queue.maxPolls


isDismissible : Entry -> Bool
isDismissible =
    Queue.isDismissible


isDone : Entry -> Bool
isDone =
    Queue.isDone


isOpen : Model -> Bool
isOpen model =
    model.open


setOpen : Bool -> Model -> Model
setOpen open model =
    { model | open = open }


pendingCount : Model -> Int
pendingCount model =
    Queue.pendingCount (toQueue model)


activeUploadId : Model -> Maybe Int
activeUploadId model =
    Queue.activeUploadId (toQueue model)


trackerKey : Int -> String
trackerKey entryId =
    "kura-upload-" ++ String.fromInt entryId


validateFile : FileInfo -> Result String ()
validateFile =
    Queue.validateFile


parseTags : String -> List String
parseTags =
    Queue.parseTags


formatBytes : Int -> String
formatBytes =
    Queue.formatBytes


phaseLabel : Queue.Phase -> String
phaseLabel =
    Queue.phaseLabel


thumbLabel : Queue.ThumbState -> String
thumbLabel =
    Queue.thumbLabel


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        BrowseClicked ->
            ( model, Select.files acceptMimes FilesPicked, [] )

        FilesPicked first rest ->
            let
                files =
                    first :: rest

                chosen =
                    Queue.filesChosen (toAccess ctx) (storedIds model) (List.map fileInfoOf files) (toQueue model)

                stored =
                    List.foldl (\( id, file ) dict -> Dict.insert id file dict)
                        model.files
                        (List.map2 Tuple.pair chosen.assigned files)
            in
            runQueue ctx { model | files = stored } chosen.model chosen.effects chosen.notes

        ViewPost postId ->
            ( model, Cmd.none, [ OpenPost postId ] )

        OpenPanel ->
            runEvent ctx model Queue.PanelOpened

        ClosePanel ->
            runEvent ctx model Queue.PanelClosed

        TagsChanged value ->
            runEvent ctx model (Queue.TagsEdited value)

        SourceChanged value ->
            runEvent ctx model (Queue.SourceEdited value)

        ArtistChanged value ->
            runEvent ctx model (Queue.ArtistEdited value)

        TransferProgress entryId progress ->
            runEvent ctx model (progressEvent entryId progress)

        UploadFinished entryId generation result ->
            runEvent ctx model
                (Queue.UploadSettled
                    { entryId = entryId
                    , generation = generation
                    , outcome =
                        case result of
                            Ok detail ->
                                Queue.CompletedUpload detail

                            Err error ->
                                Queue.FailedUpload
                                    { message = Upload.uploadErrorToString error
                                    , unauthorized =
                                        case error of
                                            Http.BadStatus 401 ->
                                                True

                                            _ ->
                                                False
                                    }
                    }
                )

        JobTick _ ->
            runEvent ctx model Queue.PollTick

        JobFinished entryId result ->
            runEvent ctx model
                (Queue.JobPolled
                    { entryId = entryId
                    , report =
                        case result of
                            Err _ ->
                                Nothing

                            Ok job ->
                                Just
                                    { succeeded = job.status == "succeeded"
                                    , terminal = Job.isTerminal job.status
                                    , status = job.status
                                    , lastError = job.lastError
                                    }
                    }
                )

        DismissEntry entryId ->
            runEvent ctx model (Queue.DismissRequested entryId)

        ClearFinished ->
            runEvent ctx model Queue.FinishedCleared


runEvent : Context -> Model -> Queue.Event -> ( Model, Cmd Msg, List OutMsg )
runEvent ctx model event =
    let
        ( queue, effects, notes ) =
            Queue.update (toAccess ctx) (storedIds model) event (toQueue model)
    in
    runQueue ctx model queue effects notes


progressEvent : Int -> Http.Progress -> Queue.Event
progressEvent entryId progress =
    case progress of
        Http.Sending sending ->
            Queue.ProgressTick { entryId = entryId, sent = sending.sent, size = sending.size, isSending = True }

        Http.Receiving _ ->
            Queue.ProgressTick { entryId = entryId, sent = 0, size = 0, isSending = False }


{-| Resume a paused queue after unlock. Queued entries start only while
writes are enabled; while locked this is a no-op so a lock can never
launch a new upload.
-}
resume : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
resume ctx model =
    let
        ( queue, effects, notes ) =
            Queue.resume (toAccess ctx) (storedIds model) (toQueue model)
    in
    runQueue ctx model queue effects notes


{-| Apply a settled pure transition: merge the queue state, release
dropped stored values, execute the described effects, and forward the
page-directed notes.
-}
runQueue : Context -> Model -> Queue.Model -> List Queue.Effect -> List Queue.Note -> ( Model, Cmd Msg, List OutMsg )
runQueue ctx model queue effects notes =
    ( fromQueue queue model |> dropStored effects
    , Cmd.batch (List.map (executeEffect ctx model.files queue) effects)
    , List.map toOutMsg notes
    )


executeEffect : Context -> Dict Int File -> Queue.Model -> Queue.Effect -> Cmd Msg
executeEffect ctx files queue effect =
    case effect of
        Queue.PickFiles ->
            Select.files acceptMimes FilesPicked

        Queue.BeginUpload entryId ->
            case ( Dict.get entryId files, List.filter (\entry -> entry.id == entryId) queue.entries |> List.head ) of
                ( Just file, Just entry ) ->
                    Upload.request ctx.apiBase
                        ctx.credential
                        (trackerKey entryId)
                        file
                        { tags = entry.tags, source = entry.source, artist = entry.artist }
                        (UploadFinished entryId ctx.accessGeneration)

                _ ->
                    Cmd.none

        Queue.FetchJob entryId jobId ->
            Job.get ctx.apiBase jobId (JobFinished entryId)

        Queue.DropFile _ ->
            Cmd.none

        Queue.DropFiles _ ->
            Cmd.none


dropStored : List Queue.Effect -> Model -> Model
dropStored effects model =
    { model | files = List.foldl Dict.remove model.files (droppedIds effects) }


droppedIds : List Queue.Effect -> List Int
droppedIds effects =
    List.concatMap
        (\effect ->
            case effect of
                Queue.DropFile entryId ->
                    [ entryId ]

                Queue.DropFiles entryIds ->
                    entryIds

                _ ->
                    []
        )
        effects


toOutMsg : Queue.Note -> OutMsg
toOutMsg note =
    case note of
        Queue.UploadConfirmed detail ->
            UploadConfirmed detail

        Queue.ThumbCompleted postId ->
            ThumbCompleted postId

        Queue.CredentialRejected generation ->
            CredentialRejected generation


toAccess : Context -> Queue.Access
toAccess ctx =
    { writesEnabled = ctx.writesEnabled
    , accessGeneration = ctx.accessGeneration
    }


toQueue : Model -> Queue.Model
toQueue model =
    { open = model.open
    , tagsDraft = model.tagsDraft
    , sourceDraft = model.sourceDraft
    , artistDraft = model.artistDraft
    , entries = model.entries
    , nextId = model.nextId
    }


fromQueue : Queue.Model -> Model -> Model
fromQueue queue model =
    { model
        | open = queue.open
        , tagsDraft = queue.tagsDraft
        , sourceDraft = queue.sourceDraft
        , artistDraft = queue.artistDraft
        , entries = queue.entries
        , nextId = queue.nextId
    }


storedIds : Model -> Set Int
storedIds model =
    Dict.keys model.files |> Set.fromList


fileInfoOf : File -> FileInfo
fileInfoOf file =
    { name = File.name file
    , mime = File.mime file
    , size = File.size file
    }


subscriptions : Model -> Sub Msg
subscriptions model =
    Sub.batch
        [ case activeUploadId model of
            Just entryId ->
                Http.track (trackerKey entryId) (TransferProgress entryId)

            Nothing ->
                Sub.none
        , if Queue.hasWaitingThumb (toQueue model) then
            Time.every pollIntervalMs JobTick

          else
            Sub.none
        ]


view : Bool -> Model -> Html Msg
view writesEnabled model =
    UploadView.view
        { onClosePanel = ClosePanel
        , onTagsChanged = TagsChanged
        , onSourceChanged = SourceChanged
        , onArtistChanged = ArtistChanged
        , onBrowse = BrowseClicked
        , onViewPost = ViewPost
        , onDismiss = DismissEntry
        , onClearFinished = ClearFinished
        }
        writesEnabled
        (toQueue model)
