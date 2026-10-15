module Feature.Recovery exposing (Context, Model, Msg(..), OutMsg(..), context, init, locked, update, view)

import Api.Recovery
import Domain.Recovery exposing (Backup, Listing, Receipt)
import Html exposing (Html, button, code, details, div, h2, li, p, section, span, summary, text, ul)
import Html.Attributes exposing (class, disabled, type_)
import Html.Events exposing (onClick)
import Http


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
    , status : Maybe String
    , requestId : Int
    }


init : Model
init =
    { open = False, backups = [], configured = False, truncated = False, loading = False, verifying = Nothing, receipt = Nothing, status = Nothing, requestId = 0 }


type Msg
    = OpenRequested
    | CloseRequested
    | ReloadRequested
    | VerifyRequested String
    | Loaded Int Int (Result Http.Error Listing)
    | Verified Int Int String (Result Http.Error Receipt)


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

            else if model.loading || model.verifying /= Nothing then
                ( model, Cmd.none, [] )

            else
                load ctx { model | receipt = Nothing }

        CloseRequested ->
            ( locked model, Cmd.none, [] )

        VerifyRequested id ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if model.loading || model.verifying /= Nothing || not (List.any (\b -> b.id == id && b.status == "available") model.backups) then
                ( model, Cmd.none, [] )

            else
                let
                    request = model.requestId + 1
                in
                ( { model | verifying = Just id, receipt = Nothing, requestId = request, status = Just "Checking every backup file hash. This can take up to two minutes…" }
                , Api.Recovery.verify ctx.apiBase ctx.credential id (Verified ctx.generation request id)
                , []
                )

        Loaded generation request result ->
            if generation /= ctx.generation || request /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok listing ->
                        ( { model | backups = listing.backups, configured = listing.configured, truncated = listing.truncated, loading = False, receipt = Nothing, status = Nothing }, Cmd.none, [] )

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
        request = model.requestId + 1
    in
    ( { model | requestId = request, loading = True, verifying = Nothing, receipt = Nothing, status = Just "Loading backup list…" }
    , Api.Recovery.list ctx.apiBase ctx.credential (Loaded ctx.generation request)
    , []
    )


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
                [ p [] [ text "Inspect local backups and explicitly verify their manifest and file hashes. Recovery actions are offline operator steps." ]
                , div [ class "recovery-actions" ] [ button [ type_ "button", disabled (model.loading || model.verifying /= Nothing), onClick ReloadRequested ] [ text "Reload list" ] ]
                , case model.status of
                    Just value -> p [ class "recovery-status" ] [ text value ]
                    Nothing -> text ""
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
        , p [] [ text "The local backup script covers PostgreSQL and local media only; configured S3 media is unsupported. Stop the Kura API, workers, and every other writer before create or restore, and keep them stopped for the whole operation. The script requires --offline as an operator acknowledgment; it does not enforce a lock." ]
        , p [] [ text "Set DATABASE_URL in the process environment to the source database for create, or to the target database for restore. Keep the URL out of command arguments and shell history. Create a new backup directory (the destination must not already exist):" ]
        , code [] [ text "python3 scripts/library-backup.py create --output /absolute/new-backup-dir --media-root /absolute/local-media-dir --offline" ]
        , p [] [ text "Verify a backup with the same full-file hash check used by this dashboard:" ]
        , code [] [ text "python3 scripts/library-backup.py verify --input /absolute/backup-dir" ]
        , p [] [ text "Restore only into an empty target database and a new media directory that does not exist. The script does not write DATABASE_URL into the backup." ]
        , code [] [ text "python3 scripts/library-backup.py restore --input /absolute/backup-dir --media-root /absolute/new-media-dir --offline" ]
        , p [] [ text "This dashboard does not create, restore, overwrite, or delete backups. A successful verification records that the files matched at that time; it does not authenticate the archive or promise future restore success." ]
        ]


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

