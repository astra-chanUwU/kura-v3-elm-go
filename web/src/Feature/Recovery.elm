module Feature.Recovery exposing (Context, Model, Msg(..), OutMsg(..), context, init, isOpen, locked, update, view)

import Api.Recovery
import Domain.Recovery exposing (Backup, Job, Listing, Receipt)
import Html exposing (Html, button, code, details, div, h2, li, p, section, span, summary, text, ul)
import Html.Attributes exposing (attribute, class, disabled, type_)
import Html.Events exposing (onClick)
import Http
import Process
import Task


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , canAdmin : Bool
    , generation : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential canAdmin generation =
    { apiBase = apiBase, credential = credential, canAdmin = canAdmin, generation = generation }


type alias Model =
    { open : Bool
    , backups : List Backup
    , configured : Bool
    , truncated : Bool
    , loading : Bool
    , verifying : Maybe String
    , receipt : Maybe Receipt
    , job : Maybe Job
    , creating : Bool
    , polls : Int
    , status : Maybe String
    , requestId : Int
    }


init : Model
init =
    { open = False, backups = [], configured = False, truncated = False, loading = False, verifying = Nothing, receipt = Nothing, job = Nothing, creating = False, polls = 0, status = Nothing, requestId = 0 }


isOpen : Model -> Bool
isOpen model =
    model.open


type Msg
    = OpenRequested
    | CloseRequested
    | ReloadRequested
    | VerifyRequested String
    | CreateRequested
    | CancelRequested
    | PollTick Int
    | Loaded Int Int (Result Http.Error Listing)
    | Verified Int Int String (Result Http.Error Receipt)
    | Created Int Int (Result Api.Recovery.JobFailure Job)
    | JobChecked Int Int (Result Api.Recovery.JobFailure Job)


type OutMsg
    = AccessRevoked


locked : Model -> Model
locked model =
    { init | requestId = model.requestId + 1 }


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenRequested ->
            if not ctx.canAdmin then
                ( { model | status = Just "Admin access is required to inspect recovery backups." }, Cmd.none, [] )

            else
                load ctx { model | open = True, receipt = Nothing }

        ReloadRequested ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if model.loading || model.verifying /= Nothing || model.creating || jobRunning model then
                ( model, Cmd.none, [] )

            else
                load ctx { model | receipt = Nothing }

        CloseRequested ->
            ( { init | requestId = model.requestId + 1, job = model.job }, Cmd.none, [] )

        VerifyRequested id ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if model.loading || model.verifying /= Nothing || model.creating || jobRunning model || not (List.any (\b -> b.id == id && b.status == "available") model.backups) then
                ( model, Cmd.none, [] )

            else
                let
                    request = model.requestId + 1
                in
                ( { model | verifying = Just id, receipt = Nothing, requestId = request, status = Just "Checking every backup file hash. This can take up to two minutes…" }
                , Api.Recovery.verify ctx.apiBase ctx.credential id (Verified ctx.generation request id)
                , []
                )

        CreateRequested ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if not model.open || not model.configured || model.loading || model.creating || model.verifying /= Nothing || jobRunning model then
                ( model, Cmd.none, [] )

            else
                let
                    request =
                        model.requestId + 1
                in
                ( { model | creating = True, requestId = request, polls = 0, status = Just "Requesting a backup. Acceptance is not publication." }
                , Api.Recovery.create ctx.apiBase ctx.credential (Created ctx.generation request)
                , []
                )

        CancelRequested ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else
                case model.job of
                    Just job ->
                        if job.status == "running" && not model.creating then
                            let
                                request =
                                    model.requestId + 1
                            in
                            ( { model | requestId = request, status = Just "Canceling the running backup. It stays incomplete until the server confirms." }
                            , Api.Recovery.cancel ctx.apiBase ctx.credential job.id (JobChecked ctx.generation request)
                            , []
                            )

                        else
                            ( model, Cmd.none, [] )

                    Nothing ->
                        ( model, Cmd.none, [] )

        PollTick request ->
            if request /= model.requestId || not model.open then
                ( model, Cmd.none, [] )

            else
                case model.job of
                    Just job ->
                        if job.status == "running" && model.polls < 900 then
                            ( { model | polls = model.polls + 1 }
                            , Api.Recovery.job ctx.apiBase ctx.credential job.id (JobChecked ctx.generation request)
                            , []
                            )

                        else if job.status == "running" then
                            ( { model | status = Just "The server has not published this backup yet. Reload before treating it as complete." }, Cmd.none, [] )

                        else
                            ( model, Cmd.none, [] )

                    Nothing ->
                        ( model, Cmd.none, [] )

        Created generation request result ->
            if generation /= ctx.generation || request /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok job ->
                        acceptJob ctx model job

                    Err failure ->
                        ( failJob model failure, Cmd.none, jobOuts failure )

        JobChecked generation request result ->
            if generation /= ctx.generation || request /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok job ->
                        acceptJob ctx model job

                    Err failure ->
                        ( failJob model failure, Cmd.none, jobOuts failure )

        Loaded generation request result ->
            if generation /= ctx.generation || request /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok listing ->
                        ( { model | backups = listing.backups, configured = listing.configured, truncated = listing.truncated, loading = False, receipt = Nothing, status = jobSummary model.job }, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( locked model, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 403) ->
                        ( locked model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | loading = False, status = Just ("Backup list could not be loaded (HTTP " ++ httpStatus error ++ ").") }, Cmd.none, [] )

        Verified generation request id result ->
            if generation /= ctx.generation || request /= model.requestId || model.verifying /= Just id then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok receipt ->
                        ( { model | verifying = Nothing, receipt = Just receipt, status = Just "Verification completed. This receipt records the check time; it does not guarantee a future restore." }, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( locked model, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 403) ->
                        ( locked model, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 409) ->
                        ( { model | verifying = Nothing, status = Just "Another verification is running. Reload the list before trying again." }, Cmd.none, [] )

                    Err (Http.Timeout) ->
                        ( { model | verifying = Nothing, status = Just "Verification timed out. Reload and explicitly verify again only when ready." }, Cmd.none, [] )

                    Err error ->
                        ( { model | verifying = Nothing, status = Just ("Backup verification failed (HTTP " ++ httpStatus error ++ "). Reload before retrying.") }, Cmd.none, [] )


load : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
load ctx model =
    let
        request =
            model.requestId + 1

        jobCmd =
            case model.job of
                Just current ->
                    if current.status == "running" then
                        Api.Recovery.job ctx.apiBase ctx.credential current.id (JobChecked ctx.generation request)

                    else
                        Cmd.none

                Nothing ->
                    Cmd.none
    in
    ( { model | requestId = request, loading = True, verifying = Nothing, receipt = Nothing, creating = False, status = Just "Loading backup list…" }
    , Cmd.batch
        [ Api.Recovery.list ctx.apiBase ctx.credential (Loaded ctx.generation request)
        , jobCmd
        ]
    , []
    )


acceptJob : Context -> Model -> Job -> ( Model, Cmd Msg, List OutMsg )
acceptJob ctx model job =
    let
        next =
            { model | creating = False, job = Just job, status = Just (jobSentence job) }
    in
    if job.status == "running" then
        ( next, schedule model.requestId, [] )

    else if job.status == "succeeded" then
        let
            request =
                model.requestId + 1
        in
        ( { next | requestId = request, loading = True }
        , Api.Recovery.list ctx.apiBase ctx.credential (Loaded ctx.generation request)
        , []
        )

    else
        ( next, Cmd.none, [] )


failJob : Model -> Api.Recovery.JobFailure -> Model
failJob model failure =
    case failure of
        Api.Recovery.JobDenied ->
            locked model

        _ ->
            { model | creating = False, loading = False, status = Just (failureText failure) }


jobOuts : Api.Recovery.JobFailure -> List OutMsg
jobOuts failure =
    case failure of
        Api.Recovery.JobDenied ->
            [ AccessRevoked ]

        _ ->
            []


schedule : Int -> Cmd Msg
schedule request =
    Process.sleep 2000 |> Task.perform (\_ -> PollTick request)


jobRunning : Model -> Bool
jobRunning model =
    case model.job of
        Just job ->
            job.status == "running"

        Nothing ->
            False


jobSummary : Maybe Job -> Maybe String
jobSummary job =
    Maybe.map jobSentence job


jobSentence : Job -> String
jobSentence job =
    case job.status of
        "running" ->
            "Backup is running. It is not complete until the server publishes it."

        "succeeded" ->
            "Backup " ++ job.id ++ " was published. Publishing is not verification. Restore stays offline."

        "canceled" ->
            "Backup canceled before publication."

        "failed" ->
            "Backup failed before publication. " ++ safeJobError job.error

        _ ->
            "Backup state is unavailable."


safeJobError : Maybe String -> String
safeJobError error =
    case error of
        Just "atomic publication is unavailable on this platform" ->
            "Atomic publication is unavailable on this platform."

        Just "referenced media is missing" ->
            "Referenced media is missing."

        Just "referenced media does not match the snapshot" ->
            "Referenced media does not match the snapshot."

        Just "database dump failed" ->
            "The database dump failed."

        Just "backup interrupted" ->
            "The backup was interrupted."

        Just "backup was interrupted before publication" ->
            "The backup was interrupted before publication."

        Just "S3 backups are not supported" ->
            "S3 backups are not supported."

        Just "a backup is already running" ->
            "A backup is already running."

        Just "backup storage is full" ->
            "Backup storage is full."

        Just "backup timed out" ->
            "The backup timed out."

        Just "backup failed" ->
            "The backup failed."

        _ ->
            "The backup failed."


failureText : Api.Recovery.JobFailure -> String
failureText failure =
    case failure of
        Api.Recovery.JobUnavailable "S3 backups are not supported" ->
            "S3 backups are not supported. Nothing was published."

        Api.Recovery.JobUnavailable "backup root is not configured" ->
            "KURA_BACKUP_ROOT is not configured on the server."

        Api.Recovery.JobUnavailable "recovery unavailable" ->
            "Recovery is unavailable. Nothing was published."

        Api.Recovery.JobUnavailable _ ->
            "Backup creation is unavailable. Nothing was published."

        Api.Recovery.JobConflict ->
            "A backup is already running. This request was not accepted."

        Api.Recovery.JobMissing ->
            "That backup request is no longer on the server."

        Api.Recovery.JobRejected "timeout" ->
            "The backup request timed out. Reload before treating it as complete."

        Api.Recovery.JobRejected "network" ->
            "The backup request could not reach the server. Reload before treating it as complete."

        Api.Recovery.JobDenied ->
            "Admin access is required."

        Api.Recovery.JobRejected _ ->
            "The backup request failed. Nothing was published."


view : Model -> Html Msg
view model =
    section [ class "recovery-view" ]
        [ div [ class "recovery-heading" ]
            [ h2 [] [ text "Recovery" ]
            , if model.open then
                button [ type_ "button", onClick CloseRequested ] [ text "Close" ]

              else
                button [ type_ "button", onClick OpenRequested ] [ text "Inspect backups" ]
            ]
        , if model.open then
            div [ class "recovery-content" ]
                [ p [] [ text "Request a local backup without stopping browsing. A request is complete only after the server publishes it. Restore stays an offline operator step." ]
                , div [ class "recovery-actions" ]
                    [ button [ type_ "button", disabled (busy model || not model.configured), onClick CreateRequested ] [ text (if model.creating then "Requesting…" else "Back up now") ]
                    , if jobRunning model then
                        button [ type_ "button", disabled model.creating, onClick CancelRequested ] [ text "Cancel backup" ]

                      else
                        text ""
                    , button [ type_ "button", disabled (busy model), onClick ReloadRequested ] [ text "Reload list" ]
                    ]
                , case model.status of
                    Just value ->
                        p [ class "recovery-status", attribute "role" "status", attribute "aria-live" "polite" ] [ text value ]

                    Nothing ->
                        text ""
                , if not model.configured && not model.loading then
                    p [ class "recovery-empty" ] [ text "KURA_BACKUP_ROOT is not configured on the server." ]

                  else
                    backupRows model
                , guide
                ]

          else
            text ""
        ]


backupRows : Model -> Html Msg
backupRows model =
    div [ class "recovery-list-wrap" ]
        [ if model.truncated then p [ class "recovery-note" ] [ text "Showing the newest 100 backup entries. Older entries are not shown." ] else text ""
        , if List.isEmpty model.backups && model.configured && not model.loading then
            p [ class "recovery-empty" ] [ text "No backup directories were found." ]

          else
            ul [ class "recovery-list" ] (List.map (backupRow model) model.backups)
        , case model.receipt of
            Just receipt -> p [ class "recovery-receipt" ] [ text ("Verified " ++ receipt.id ++ " at " ++ receipt.verifiedAt ++ ". Checked " ++ String.fromInt receipt.mediaCount ++ " media files and " ++ formatBytes receipt.bytes ++ ".") ]
            Nothing -> text ""
        ]


backupRow : Model -> Backup -> Html Msg
backupRow model backup =
    li [ class "recovery-row" ]
        [ div [ class "recovery-meta" ]
            [ span [ class "recovery-id" ] [ text backup.id ]
            , span [] [ text (backup.createdAt ++ " · " ++ String.fromInt backup.mediaCount ++ " media · " ++ formatBytes backup.bytes) ]
            , span [ class ("recovery-badge recovery-" ++ backup.status) ] [ text backup.status ]
            ]
        , button [ type_ "button", disabled (model.loading || model.verifying /= Nothing || backup.status /= "available"), onClick (VerifyRequested backup.id) ] [ text (if model.verifying == Just backup.id then "Verifying…" else "Verify files") ]
        ]


guide : Html Msg
guide =
    details [ class "recovery-guide" ]
        [ summary [] [ text "Offline backup and restore guide" ]
        , p [] [ text "Back up now uses one PostgreSQL snapshot and does not stop browsing or workers. S3 backups are unsupported. Offline restore still requires every writer to stay stopped, and the script's --offline flag is only an operator acknowledgment." ]
        , p [] [ text "Set DATABASE_URL in the process environment to the source database for create, or to the target database for restore. Keep the URL out of command arguments and shell history. Create a new backup directory (the destination must not already exist):" ]
        , code [] [ text "python3 scripts/library-backup.py create --output /absolute/new-backup-dir --media-root /absolute/local-media-dir --offline" ]
        , p [] [ text "Verify a backup with the same full-file hash check used by this dashboard:" ]
        , code [] [ text "python3 scripts/library-backup.py verify --input /absolute/backup-dir" ]
        , p [] [ text "Restore only into an empty target database and a new media directory that does not exist. The script does not write DATABASE_URL into the backup." ]
        , code [] [ text "python3 scripts/library-backup.py restore --input /absolute/backup-dir --media-root /absolute/new-media-dir --offline" ]
        , p [] [ text "This dashboard can request and verify a local backup. It does not restore, overwrite, or delete one. A successful verification records that the files matched at that time; it does not authenticate the archive or promise future restore success." ]
        ]


busy : Model -> Bool
busy model =
    model.loading || model.creating || model.verifying /= Nothing || jobRunning model


formatBytes : Float -> String
formatBytes bytes =
    if bytes < 1024 then
        displayNumber bytes ++ " B"

    else if bytes < 1024 * 1024 then
        displayNumber (toFloat (floor (bytes / 1024))) ++ " KiB"

    else
        displayNumber (toFloat (floor (bytes / (1024 * 1024)))) ++ " MiB"


displayNumber : Float -> String
displayNumber number =
    let
        raw =
            String.fromFloat number
    in
    if String.endsWith ".0" raw then
        String.dropRight 2 raw

    else
        raw


httpStatus : Http.Error -> String
httpStatus error =
    case error of
        Http.BadStatus status ->
            String.fromInt status

        Http.Timeout ->
            "timeout"

        Http.NetworkError ->
            "network"

        Http.BadUrl _ ->
            "request"

        Http.BadBody _ ->
            "response"
