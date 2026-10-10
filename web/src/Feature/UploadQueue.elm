module Feature.UploadQueue exposing
    ( Context
    , Entry
    , FileInfo
    , Model
    , Msg(..)
    , OutMsg(..)
    , Phase(..)
    , ThumbState(..)
    , UploadedInfo
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

{-| Small sequential upload queue.

Files are picked through `File.Select`, validated in Elm (JPEG, PNG, GIF up
to 25 MiB), then uploaded one at a time with real transfer progress. The
phase path is Queued → Uploading → Saving → Uploaded: `Uploading` tracks
bytes sent, `Saving` means the request left the browser and the queue is
waiting on the server's persistence confirmation. A rejected file never
starts and never blocks the rest of the queue.

After the server confirms persistence, thumbnail processing is polled
separately with bounded requests that stop at terminal job states. A
thumbnail failure keeps the entry `Uploaded`: the original was saved.

Uploads are never retried automatically. After a lost response the post may
already exist, so resubmission stays an explicit user decision.
-}

import Api.Job as Job exposing (Job)
import Api.Upload as Upload
import Dict exposing (Dict)
import Domain.Post exposing (PostDetail)
import File exposing (File)
import File.Select as Select
import Html exposing (Html, button, div, h2, input, label, li, p, span, text, ul)
import Html.Attributes as Attr exposing (attribute, class, disabled, id, placeholder, title, type_, value)
import Html.Events exposing (onClick, onInput)
import Http
import Time


{-| Client-side bound mirroring the server: 25 MiB per file.
-}
maxBytes : Int
maxBytes =
    Upload.maxBytes


{-| Picker hint. Validation still runs in Elm so files chosen by other
means get honest errors instead of silent acceptance.
-}
acceptMimes : List String
acceptMimes =
    [ "image/jpeg", "image/png", "image/gif" ]


{-| Thumbnail poll cadence and bound: 1.5s up to 40 polls (~60s), then
the entry settles as a thumbnail failure while staying Uploaded.
-}
pollIntervalMs : Float
pollIntervalMs =
    1500


maxPolls : Int
maxPolls =
    40


{-| Plain file facts. Validation runs on this record (instead of `File`
directly) so the rules stay unit-testable.
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


{-| Queue entries hold metadata and phase; the picked `File` values live
in `files` keyed by entry id. Separating the two keeps the state machine
testable (entries are plain data) and lets dismissal drop a file without
touching the phase of any other entry.
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
    | JobFinished Int (Result Http.Error Job)
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


{-| Only settled or never-started entries may be dismissed. Active
`Uploading`/`Saving` entries keep their request and tracker until the
response settles; the update rule below enforces this even if a dismiss
message arrives from anywhere else.
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


isOpen : Model -> Bool
isOpen model =
    model.open


setOpen : Bool -> Model -> Model
setOpen open model =
    { model | open = open }


trackerKey : Int -> String
trackerKey entryId =
    "kura-upload-" ++ String.fromInt entryId



-- VALIDATION


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
    [ ".jpg", ".jpeg", ".png", ".gif" ]


{-| Accept JPEG, PNG, and GIF up to 25 MiB. An empty MIME type falls back
to the file extension; anything else is rejected with a useful message.
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
                Err "The file is not a supported image. Upload JPEG, PNG, or GIF."

        else if List.member mime acceptMimes then
            Ok ()

        else
            Err ("Files of type " ++ info.mime ++ " are not supported. Upload JPEG, PNG, or GIF.")


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



-- UPDATE


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenPanel ->
            ( { model | open = True }, Cmd.none, [] )

        ClosePanel ->
            ( { model | open = False }, Cmd.none, [] )

        TagsChanged value ->
            ( { model | tagsDraft = value }, Cmd.none, [] )

        SourceChanged value ->
            ( { model | sourceDraft = value }, Cmd.none, [] )

        ArtistChanged value ->
            ( { model | artistDraft = value }, Cmd.none, [] )

        BrowseClicked ->
            ( model, Select.files acceptMimes FilesPicked, [] )

        FilesPicked first rest ->
            addFiles ctx model (first :: rest)

        TransferProgress entryId progress ->
            ( { model | entries = List.map (applyProgress entryId progress) model.entries }, Cmd.none, [] )

        UploadFinished entryId requestGeneration result ->
            finishUpload ctx model entryId requestGeneration result

        JobTick _ ->
            pollThumbs ctx model

        JobFinished entryId result ->
            ( { model | entries = List.map (applyJobResult entryId result) model.entries }
            , Cmd.none
            , outFromJob model entryId result
            )

        ViewPost postId ->
            ( model, Cmd.none, [ OpenPost postId ] )

        DismissEntry entryId ->
            case List.filter (\entry -> entry.id == entryId) model.entries |> List.head of
                Just entry ->
                    if isActivePhase entry.phase then
                        -- Refused in update, not just in the view: the HTTP
                        -- request and its tracker stay alive until the
                        -- response settles.
                        ( model, Cmd.none, [] )

                    else
                        pump ctx
                            { model
                                | entries = List.filter (\candidate -> candidate.id /= entryId) model.entries
                                , files = Dict.remove entryId model.files
                            }

                Nothing ->
                    ( model, Cmd.none, [] )

        ClearFinished ->
            let
                kept =
                    List.filter (not << isDoneEntry) model.entries

                keptIds =
                    List.map .id kept
            in
            ( { model
                | entries = kept
                , files = Dict.filter (\key _ -> List.member key keptIds) model.files
              }
            , Cmd.none
            , []
            )


{-| Resume a paused queue after unlock. Queued entries start only while
writes are enabled; while locked this is a no-op so a lock can never
launch a new upload. In-flight entries are untouched either way: the
browser cannot cancel their requests, and their completions still
settle honestly through `UploadFinished`.
-}
resume : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
resume ctx model =
    if ctx.writesEnabled then
        pump ctx model

    else
        ( model, Cmd.none, [] )


fileInfoOf : File -> FileInfo
fileInfoOf file =
    { name = File.name file
    , mime = File.mime file
    , size = File.size file
    }


addFiles : Context -> Model -> List File -> ( Model, Cmd Msg, List OutMsg )
addFiles ctx model files =
    let
        sharedTags =
            parseTags model.tagsDraft

        sharedSource =
            String.trim model.sourceDraft

        sharedArtist =
            String.trim model.artistDraft

        toEntry file entryId =
            let
                info =
                    fileInfoOf file
            in
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
            List.indexedMap (\offset file -> toEntry file (model.nextId + offset)) files

        stored =
            List.foldl (\( entryId, file ) dict -> Dict.insert entryId file dict)
                model.files
                (List.map2 Tuple.pair (List.map .id entries) files)
    in
    pump ctx { model | entries = model.entries ++ entries, files = stored, nextId = model.nextId + List.length files }


{-| Start the first queued entry when nothing is in flight and writes are
enabled. While locked the queue holds its place: queued entries wait for
`resume` instead of launching uncredentialed uploads that a gated server
would reject. The context threads through from the caller; the queue
itself never stores the credential.
-}
pump : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
pump ctx model =
    startNext ctx model |> (\( m, c ) -> ( m, c, [] ))


startNext : Context -> Model -> ( Model, Cmd Msg )
startNext ctx model =
    if not ctx.writesEnabled then
        ( model, Cmd.none )

    else
        case ( activeEntry model, List.filter (\entry -> entry.phase == Queued) model.entries |> List.head ) of
            ( Nothing, Just next ) ->
                case Dict.get next.id model.files of
                    Just file ->
                        ( { model | entries = List.map (beginUpload next.id ctx.accessGeneration) model.entries }
                        , Upload.request ctx.apiBase
                            ctx.credential
                            (trackerKey next.id)
                            file
                            { tags = next.tags, source = next.source, artist = next.artist }
                            (UploadFinished next.id ctx.accessGeneration)
                        )

                    Nothing ->
                        -- The picked file is gone; fail this entry and move on
                        -- rather than stalling the queue behind it.
                        startNext ctx
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
                ( model, Cmd.none )


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


activeUploadId : Model -> Maybe Int
activeUploadId model =
    Maybe.map .id (activeEntry model)


applyProgress : Int -> Http.Progress -> Entry -> Entry
applyProgress entryId progress entry =
    if entry.id /= entryId then
        entry

    else
        case ( entry.phase, progress ) of
            ( Uploading current, Http.Sending sending ) ->
                if sending.sent >= sending.size then
                    { entry | phase = Saving current.accessGeneration }

                else
                    { entry
                        | phase =
                            Uploading
                                { sent = sending.sent
                                , size = sending.size
                                , accessGeneration = current.accessGeneration
                                }
                    }

            ( Saving accessGeneration, Http.Sending sending ) ->
                if sending.sent < sending.size then
                    { entry
                        | phase =
                            Uploading
                                { sent = sending.sent
                                , size = sending.size
                                , accessGeneration = accessGeneration
                                }
                    }

                else
                    entry

            ( Uploading current, Http.Receiving _ ) ->
                { entry | phase = Saving current.accessGeneration }

            _ ->
                entry


finishUpload : Context -> Model -> Int -> Int -> Result Http.Error PostDetail -> ( Model, Cmd Msg, List OutMsg )
finishUpload ctx model entryId requestGeneration result =
    case List.filter (\entry -> entry.id == entryId) model.entries |> List.head of
        Nothing ->
            ( model, Cmd.none, [] )

        Just entry ->
            case entry.phase of
                Uploading active ->
                    if active.accessGeneration == requestGeneration then
                        settleUpload ctx model entry requestGeneration result

                    else
                        ( model, Cmd.none, [] )

                Saving activeGeneration ->
                    if activeGeneration == requestGeneration then
                        settleUpload ctx model entry requestGeneration result

                    else
                        ( model, Cmd.none, [] )

                _ ->
                    ( model, Cmd.none, [] )


{-| Settle one upload honestly. A confirmed upload stands even when the
browser locked mid-flight: lock is not rollback, and the entry keeps
its server-confirmed post id. A 401 additionally reports
`CredentialRejected` so the Library can lock. Failures never resend:
after a lost response the post may already exist, so resubmission stays
an explicit user decision.
-}
settleUpload : Context -> Model -> Entry -> Int -> Result Http.Error PostDetail -> ( Model, Cmd Msg, List OutMsg )
settleUpload ctx model entry requestGeneration result =
    case result of
        Ok detail ->
            let
                uploaded =
                    { entry
                        | phase =
                            Uploaded
                                { postId = detail.id
                                , jobId = detail.derivativeJobId
                                , thumb =
                                    case detail.derivativeJobId of
                                        Nothing ->
                                            ThumbReady

                                        Just _ ->
                                            case detail.derivativeStatus of
                                                Just status ->
                                                    if Job.isTerminal status then
                                                        if status == "succeeded" then
                                                            ThumbReady

                                                        else
                                                            ThumbFailed ("Thumbnail " ++ status ++ " — the original was saved.")

                                                    else
                                                        ThumbWaiting 0

                                                Nothing ->
                                                    ThumbWaiting 0
                                }
                    }

                withSettled =
                    { model
                        | entries = List.map (\candidate -> if candidate.id == entry.id then uploaded else candidate) model.entries
                        , files = Dict.remove entry.id model.files
                    }
            in
            pump ctx withSettled
                |> (\( pumped, cmd, _ ) -> ( pumped, cmd, [ UploadConfirmed detail ] ))

        Err error ->
            let
                failed =
                    { entry | phase = Failed (Upload.uploadErrorToString error) }

                withFailed =
                    { model
                        | entries = List.map (\candidate -> if candidate.id == entry.id then failed else candidate) model.entries
                        , files = Dict.remove entry.id model.files
                    }

                outs =
                    case error of
                        Http.BadStatus 401 ->
                            [ CredentialRejected requestGeneration ]

                        _ ->
                            []
            in
            case error of
                Http.BadStatus 401 ->
                    if ctx.accessGeneration /= requestGeneration && ctx.writesEnabled then
                        pump ctx withFailed
                            |> (\( pumped, cmd, _ ) -> ( pumped, cmd, outs ))

                    else
                        ( withFailed, Cmd.none, outs )

                _ ->
                    pump ctx withFailed
                        |> (\( pumped, cmd, _ ) -> ( pumped, cmd, outs ))


pollThumbs : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
pollThumbs ctx model =
    let
        ( entries, cmds ) =
            List.foldr
                (\entry ( kept, commands ) ->
                    case entry.phase of
                        Uploaded info ->
                            case info.thumb of
                                ThumbWaiting polls ->
                                    if polls >= maxPolls then
                                        ( { entry | phase = Uploaded { info | thumb = ThumbFailed "Thumbnail status timed out. The original was saved." } } :: kept
                                        , commands
                                        )

                                    else
                                        case info.jobId of
                                            Just jobId ->
                                                ( { entry | phase = Uploaded { info | thumb = ThumbWaiting (polls + 1) } } :: kept
                                                , Job.get ctx.apiBase jobId (JobFinished entry.id) :: commands
                                                )

                                            Nothing ->
                                                ( { entry | phase = Uploaded { info | thumb = ThumbReady } } :: kept
                                                , commands
                                                )

                                _ ->
                                    ( entry :: kept, commands )

                        _ ->
                            ( entry :: kept, commands )
                )
                ( [], [] )
                model.entries
    in
    ( { model | entries = entries }, Cmd.batch cmds, [] )


applyJobResult : Int -> Result Http.Error Job -> Entry -> Entry
applyJobResult entryId result entry =
    if entry.id /= entryId then
        entry

    else
        case entry.phase of
            Uploaded info ->
                case info.thumb of
                    ThumbWaiting _ ->
                        case result of
                            Ok job ->
                                if Job.isTerminal job.status then
                                    if job.status == "succeeded" then
                                        { entry | phase = Uploaded { info | thumb = ThumbReady } }

                                    else
                                        { entry
                                            | phase =
                                                Uploaded
                                                    { info
                                                        | thumb =
                                                            ThumbFailed
                                                                (if String.trim job.lastError /= "" then
                                                                    "Thumbnail failed: " ++ job.lastError ++ " — the original was saved."

                                                                 else
                                                                    "Thumbnail " ++ job.status ++ " — the original was saved."
                                                                )
                                                    }
                                        }

                                else
                                    entry

                            Err _ ->
                                -- Transient poll failure: keep waiting within the poll bound.
                                entry

                    _ ->
                        entry

            _ ->
                entry


outFromJob : Model -> Int -> Result Http.Error Job -> List OutMsg
outFromJob model entryId result =
    case result of
        Ok job ->
            if job.status == "succeeded" then
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

        Err _ ->
            []


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



-- SUBSCRIPTIONS


subscriptions : Model -> Sub Msg
subscriptions model =
    Sub.batch
        [ case activeUploadId model of
            Just entryId ->
                Http.track (trackerKey entryId) (TransferProgress entryId)

            Nothing ->
                Sub.none
        , if List.any isWaitingThumb model.entries then
            Time.every pollIntervalMs JobTick

          else
            Sub.none
        ]


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



-- VIEW


{-| The panel stays inspectable while locked (finished entries keep
their thumbnails and post links), but picking new files needs writes.
-}
view : Bool -> Model -> Html Msg
view writesEnabled model =
    if not model.open then
        text ""

    else
        div [ class "upload-panel", attribute "role" "dialog", attribute "aria-label" "Upload images" ]
            [ div [ class "panel-header" ]
                [ h2 [ class "panel-title", id "upload-panel-title", attribute "tabindex" "-1" ] [ text "Upload" ]
                , button [ class "button button-quiet", type_ "button", onClick ClosePanel, attribute "aria-label" "Close upload panel" ] [ text "×" ]
                ]
            , div [ class "panel-body" ]
                [ div [ class "panel-section" ]
                    [ div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "upload-tags" ] [ text "Tags (shared)" ]
                        , input
                            [ class "upload-input"
                            , id "upload-tags"
                            , type_ "text"
                            , placeholder "cat, demo"
                            , value model.tagsDraft
                            , onInput TagsChanged
                            ]
                            []
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "upload-source" ] [ text "Source (shared, optional)" ]
                        , input
                            [ class "upload-input"
                            , id "upload-source"
                            , type_ "text"
                            , placeholder "imports/sample.jpg"
                            , value model.sourceDraft
                            , onInput SourceChanged
                            ]
                            []
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "upload-artist" ] [ text "Artist (shared, optional)" ]
                        , input
                            [ class "upload-input"
                            , id "upload-artist"
                            , type_ "text"
                            , placeholder "Kura"
                            , value model.artistDraft
                            , onInput ArtistChanged
                            ]
                            []
                        ]
                    , p [ class "panel-note" ] [ text "Shared fields apply to files you pick next. JPEG, PNG, or GIF up to 25 MiB each." ]
                    , if writesEnabled then
                        button [ class "button", type_ "button", id "upload-browse", onClick BrowseClicked ] [ text "Choose files…" ]

                      else
                        div []
                            [ button [ class "button", type_ "button", id "upload-browse", disabled True, title "Unlock to upload images." ] [ text "Choose files…" ]
                            , p [ class "panel-note" ] [ text "Unlock to upload images." ]
                            ]
                    ]
                , div [ class "panel-section" ]
                    [ h2 [ class "panel-section-title" ] [ text (queueHeading model) ]
                    , queueList model
                    , if List.any isDoneEntry model.entries then
                        button [ class "button button-quiet", type_ "button", onClick ClearFinished ] [ text "Clear finished" ]

                      else
                        text ""
                    ]
                ]
            ]


queueHeading : Model -> String
queueHeading model =
    let
        total =
            List.length model.entries

        pending =
            pendingCount model
    in
    if total == 0 then
        "Queue"

    else if pending == 0 then
        "Queue (" ++ String.fromInt total ++ " finished)"

    else
        "Queue (" ++ String.fromInt pending ++ " of " ++ String.fromInt total ++ " pending)"


queueList : Model -> Html Msg
queueList model =
    if List.isEmpty model.entries then
        p [ class "panel-note" ] [ text "No files picked yet." ]

    else
        ul [ class "upload-queue" ] (List.map entryView model.entries)


entryView : Entry -> Html Msg
entryView entry =
    li [ class "upload-item" ]
        [ div [ class "upload-item-head" ]
            [ span [ class "upload-name", title entry.name ] [ text entry.name ]
            , span [ class "upload-size" ] [ text (formatBytes entry.size) ]
            , span [ class "upload-phase", attribute "data-phase" (phaseLabel entry.phase) ] [ text (phaseLabel entry.phase) ]
            ]
        , phaseDetail entry
        , div [ class "upload-item-actions" ]
            [ case entry.phase of
                Uploaded info ->
                    button [ class "button button-quiet", type_ "button", onClick (ViewPost info.postId) ] [ text "View" ]

                _ ->
                    text ""
            , if isDismissible entry then
                button [ class "button button-quiet", type_ "button", onClick (DismissEntry entry.id), attribute "aria-label" ("Dismiss " ++ entry.name) ] [ text "Dismiss" ]

              else
                button [ class "button button-quiet", type_ "button", disabled True, title "Cannot dismiss while the upload is in flight.", attribute "aria-label" ("Dismiss " ++ entry.name ++ " (unavailable while uploading)") ] [ text "Dismiss" ]
            ]
        ]


phaseDetail : Entry -> Html Msg
phaseDetail entry =
    case entry.phase of
        Queued ->
            text ""

        Uploading sending ->
            let
                fraction =
                    if sending.size <= 0 then
                        0

                    else
                        clamp 0 100 (sending.sent * 100 // sending.size)
            in
            div [ class "upload-progress" ]
                [ Html.progress
                    [ class "upload-bar"
                    , value (String.fromInt sending.sent)
                    , Attr.max (String.fromInt (max 1 sending.size))
                    , attribute "aria-label" ("Upload progress for " ++ entry.name)
                    ]
                    []
                , span [ class "upload-percent" ] [ text (String.fromInt fraction ++ "%") ]
                ]

        Saving _ ->
            p [ class "upload-note" ] [ text "Saving to the Library…" ]

        Uploaded info ->
            let
                detail =
                    case info.thumb of
                        ThumbWaiting _ ->
                            "Thumbnail: processing…"

                        ThumbReady ->
                            "Thumbnail: ready"

                        ThumbFailed message ->
                            message
            in
            div []
                [ p [ class "upload-note" ] [ text ("Saved as post #" ++ info.postId ++ ".") ]
                , p [ class "upload-note" ] [ text detail ]
                ]

        Rejected message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]

        Failed message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]
