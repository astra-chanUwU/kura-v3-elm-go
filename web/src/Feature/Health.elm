module Feature.Health exposing (Context, Model, Msg(..), OutMsg(..), context, countLabel, init, isOpen, locked, safeScanNote, update, view)

import Api.Health
import Domain.Health as Health exposing (Page, Repair, Scan, Summary)
import Html exposing (Html, button, div, h2, li, p, section, text, ul)
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
    , loading : Bool
    , repairing : Bool
    , summary : Maybe Summary
    , scan : Scan
    , thumbnails : List String
    , thumbnailsTruncated : Bool
    , status : Maybe String
    , requestId : Int
    }


init : Model
init =
    { open = False
    , loading = False
    , repairing = False
    , summary = Nothing
    , scan = idleScan
    , thumbnails = []
    , thumbnailsTruncated = False
    , status = Nothing
    , requestId = 0
    }


idleScan : Scan
idleScan =
    { status = "idle", checked = 0, missing = 0, corrupt = 0, bytes = 0, truncated = False, error = "" }


isOpen : Model -> Bool
isOpen model =
    model.open


locked : Model -> Model
locked model =
    { init | requestId = model.requestId + 1 }


type Msg
    = OpenRequested
    | CloseRequested
    | ReloadRequested
    | StartScanRequested
    | CancelScanRequested
    | RepairRequested String
    | SummaryLoaded Int Int (Result Http.Error Summary)
    | ScanLoaded Int Int (Result Http.Error Scan)
    | ThumbnailsLoaded Int Int (Result Http.Error Page)
    | ScanStarted Int Int (Result Http.Error Scan)
    | ScanCanceled Int Int (Result Http.Error Scan)
    | Repaired Int Int (Result Http.Error Repair)


type OutMsg
    = AccessRevoked


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenRequested ->
            if not ctx.canAdmin then
                ( { model | status = Just "Admin access is required to view library health." }, Cmd.none, [] )

            else
                refresh ctx { model | open = True }

        CloseRequested ->
            ( { init | requestId = model.requestId + 1 }, Cmd.none, [] )

        ReloadRequested ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if not model.open || model.loading || model.repairing then
                ( model, Cmd.none, [] )

            else
                refresh ctx model

        StartScanRequested ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if s3Unsupported model then
                ( { model | status = Just "S3 media checks are not supported." }, Cmd.none, [] )

            else if not model.open || model.loading || model.repairing || model.scan.status == "running" then
                ( model, Cmd.none, [] )

            else
                let
                    request =
                        model.requestId + 1
                in
                ( { model | requestId = request, loading = True, status = Just "Starting a bounded local scan." }
                , Api.Health.startScan ctx.apiBase ctx.credential (ScanStarted ctx.generation request)
                , []
                )

        CancelScanRequested ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if not model.open || model.scan.status /= "running" || model.loading then
                ( model, Cmd.none, [] )

            else
                let
                    request =
                        model.requestId + 1
                in
                ( { model | requestId = request, loading = True, status = Just "Canceling the library scan." }
                , Api.Health.cancelScan ctx.apiBase ctx.credential (ScanCanceled ctx.generation request)
                , []
                )

        RepairRequested postId ->
            if not ctx.canAdmin then
                ( locked model, Cmd.none, [ AccessRevoked ] )

            else if not model.open || model.loading || model.repairing || not (List.member postId model.thumbnails) then
                ( model, Cmd.none, [] )

            else
                let
                    request =
                        model.requestId + 1
                in
                ( { model | requestId = request, repairing = True, status = Just "Queueing one thumbnail repair. The original stays in place." }
                , Api.Health.repair ctx.apiBase ctx.credential postId (Repaired ctx.generation request)
                , []
                )

        SummaryLoaded generation request result ->
            accept ctx generation request model <|
                \() ->
                    case result of
                        Ok summary ->
                            ( { model | loading = False, summary = Just summary, status = summaryStatus summary }, Cmd.none, [] )

                        Err error ->
                            fail error model

        ScanLoaded generation request result ->
            accept ctx generation request model <|
                \() ->
                    case result of
                        Ok scan ->
                            ( { model | scan = scan }, Cmd.none, [] )

                        Err error ->
                            fail error model

        ThumbnailsLoaded generation request result ->
            accept ctx generation request model <|
                \() ->
                    case result of
                        Ok page ->
                            ( { model | thumbnails = page.postIds, thumbnailsTruncated = page.truncated }, Cmd.none, [] )

                        Err error ->
                            fail error model

        ScanStarted generation request result ->
            accept ctx generation request model <|
                \() ->
                    case result of
                        Ok scan ->
                            ( { model | loading = False, scan = scan, status = Just "Scan started. Reload to read the result." }, Cmd.none, [] )

                        Err error ->
                            fail error model

        ScanCanceled generation request result ->
            accept ctx generation request model <|
                \() ->
                    case result of
                        Ok scan ->
                            ( { model | loading = False, scan = scan, status = Just "Scan canceled." }, Cmd.none, [] )

                        Err error ->
                            fail error model

        Repaired generation request result ->
            accept ctx generation request model <|
                \() ->
                    case result of
                        Ok repair ->
                            ( { model | repairing = False, status = Just (repairStatus repair) }, Cmd.none, [] )

                        Err error ->
                            fail error model


accept : Context -> Int -> Int -> Model -> (() -> ( Model, Cmd Msg, List OutMsg )) -> ( Model, Cmd Msg, List OutMsg )
accept ctx generation request model continue =
    if generation /= ctx.generation || request /= model.requestId || not model.open then
        ( model, Cmd.none, [] )

    else
        continue ()


refresh : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
refresh ctx model =
    let
        request =
            model.requestId + 1
    in
    ( { model | requestId = request, loading = True, status = Just "Loading library health." }
    , Cmd.batch
        [ Api.Health.summary ctx.apiBase ctx.credential (SummaryLoaded ctx.generation request)
        , Api.Health.scan ctx.apiBase ctx.credential (ScanLoaded ctx.generation request)
        , Api.Health.thumbnails ctx.apiBase ctx.credential (ThumbnailsLoaded ctx.generation request)
        ]
    , []
    )


fail : Http.Error -> Model -> ( Model, Cmd Msg, List OutMsg )
fail error model =
    case error of
        Http.BadStatus 401 ->
            ( locked model, Cmd.none, [ AccessRevoked ] )

        Http.BadStatus 403 ->
            ( locked model, Cmd.none, [ AccessRevoked ] )

        Http.BadStatus 409 ->
            ( { model | loading = False, repairing = False, status = Just "That library health request conflicts with one already running." }, Cmd.none, [] )

        Http.BadStatus 503 ->
            ( { model | loading = False, repairing = False, status = Just "Library health is unavailable." }, Cmd.none, [] )

        _ ->
            ( { model | loading = False, repairing = False, status = Just "The library health request failed." }, Cmd.none, [] )


summaryStatus : Summary -> Maybe String
summaryStatus summary =
    if summary.s3Checks == "unsupported" then
        Just "S3 media checks are not supported. Missing originals and storage size stay unknown."

    else
        Nothing


s3Unsupported : Model -> Bool
s3Unsupported model =
    model.summary |> Maybe.map (\summary -> summary.s3Checks == "unsupported") |> Maybe.withDefault False


repairStatus : Repair -> String
repairStatus repair =
    "Thumbnail repair " ++ repair.jobId ++ " is " ++ knownJobStatus repair.status ++ ". The original was not changed."


knownJobStatus : String -> String
knownJobStatus status =
    if List.member status [ "pending", "running", "succeeded", "failed", "cancelled" ] then
        status

    else
        "accepted"


countLabel : Bool -> Int -> String
countLabel known value =
    if known then
        String.fromInt value

    else
        "unknown"


bytesLabel : Health.KnownBytes -> String
bytesLabel bytes =
    if bytes.known then
        String.fromInt (floor bytes.value) ++ " bytes"

    else
        "unknown"


safeScanNote : String -> Maybe String
safeScanNote raw =
    if List.member raw [ "media check failed", "library scan failed" ] then
        Just raw

    else
        Nothing


view : Model -> Html Msg
view model =
    section [ class "health-view" ]
        [ div [ class "health-heading" ]
            [ h2 [] [ text "Library health" ]
            , if model.open then
                button [ type_ "button", onClick CloseRequested ] [ text "Close" ]

              else
                button [ type_ "button", onClick OpenRequested ] [ text "Inspect library" ]
            ]
        , if model.open then
            div [ class "health-content" ]
                [ p [] [ text "Counts are bounded. A storage scan is separate from the server liveness check and does not rewrite or delete originals." ]
                , inventory model
                , div [ class "health-actions" ]
                    [ button [ type_ "button", disabled (model.loading || model.repairing || model.scan.status == "running" || s3Unsupported model), onClick StartScanRequested ] [ text "Scan local files" ]
                    , if model.scan.status == "running" then
                        button [ type_ "button", disabled model.loading, onClick CancelScanRequested ] [ text "Cancel scan" ]

                      else
                        text ""
                    , button [ type_ "button", disabled (model.loading || model.repairing), onClick ReloadRequested ] [ text "Reload" ]
                    ]
                , scanLine model
                , thumbnailList model
                , case model.status of
                    Just value ->
                        p [ class "health-status" ] [ text value ]

                    Nothing ->
                        text ""
                ]

          else
            text ""
        ]


inventory : Model -> Html Msg
inventory model =
    case model.summary of
        Nothing ->
            p [] [ text "Library health has not loaded." ]

        Just summary ->
            ul [ class "health-counts" ]
                [ li [] [ text ("Missing originals: " ++ countLabel summary.missingOriginals.known summary.missingOriginals.value) ]
                , li [] [ text ("Missing thumbnails: " ++ countLabel summary.missingThumbnails.known summary.missingThumbnails.value) ]
                , li [] [ text ("Failed thumbnails: " ++ countLabel summary.failedThumbnails.known summary.failedThumbnails.value) ]
                , li [] [ text ("Processing backlog: " ++ countLabel summary.processingBacklog.known summary.processingBacklog.value) ]
                , li [] [ text ("Storage: " ++ bytesLabel summary.storageBytes) ]
                , li [] [ text (backupLine summary) ]
                ]


backupLine : Summary -> String
backupLine summary =
    case summary.latestVerifiedBackup of
        Just backup ->
            "Latest verified backup: " ++ backup.id ++ " at " ++ backup.createdAt

        Nothing ->
            "Latest verified backup: none"


scanLine : Model -> Html Msg
scanLine model =
    p []
        [ text
            ("Scan " ++ model.scan.status ++ ". Checked " ++ String.fromInt model.scan.checked ++ ", missing " ++ String.fromInt model.scan.missing ++ ", empty " ++ String.fromInt model.scan.corrupt ++ ".")
        , if model.scan.truncated then
            text " Showing the first 200 objects."

          else
            text ""
        , case safeScanNote model.scan.error of
            Just note ->
                text (" " ++ note)

            Nothing ->
                text ""
        ]


thumbnailList : Model -> Html Msg
thumbnailList model =
    div []
        [ if model.thumbnailsTruncated then
            p [] [ text "Showing the first 50 posts with no ready thumbnail." ]

          else
            text ""
        , if List.isEmpty model.thumbnails then
            p [] [ text "No missing thumbnails in this page." ]

          else
            ul [ class "health-list" ] (List.map (thumbnailRow model) model.thumbnails)
        ]


thumbnailRow : Model -> String -> Html Msg
thumbnailRow model postId =
    li [ class "health-row" ]
        [ text ("Post " ++ postId)
        , button [ type_ "button", disabled (model.loading || model.repairing), onClick (RepairRequested postId) ] [ text "Repair thumbnail" ]
        ]
