module RecoveryTests exposing (suite)

import Api.Recovery
import Domain.Recovery as Recovery
import Feature.Recovery as Feature
import Http
import Json.Decode as Decode


ctx : Feature.Context
ctx =
    Feature.context "" (Just "secret") True 4


step : Feature.Context -> Feature.Msg -> Feature.Model -> ( Feature.Model, List Feature.OutMsg )
step context msg model =
    let
        ( next, _, outs ) =
            Feature.update context msg model
    in
    ( next, outs )


suite : List ( String, Bool )
suite =
    [ ( "recovery listing is bounded and decodes metadata only"
      , case Decode.decodeString Recovery.listingDecoder """{"configured":true,"backups":[{"id":"nightly-1","created_at":"2026-10-15T12:00:00Z","media_count":2,"bytes":100,"status":"available"}],"truncated":false}""" of
            Ok listing -> listing.configured && List.length listing.backups == 1 && (List.head listing.backups |> Maybe.map .id) == Just "nightly-1"
            Err _ -> False
      )
    , ( "recovery rejects unknown status and over-limit listings"
      , let
            row =
                "{\"id\":\"a\",\"created_at\":\"now\",\"media_count\":0,\"bytes\":0,\"status\":\"available\"}"

            tooMany =
                "{\"configured\":true,\"backups\":[" ++ String.join "," (List.repeat 101 row) ++ "],\"truncated\":false}"
        in
        (case Decode.decodeString Recovery.listingDecoder """{"configured":true,"backups":[{"id":"x","created_at":"now","media_count":0,"bytes":0,"status":"verified"}],"truncated":false}""" of
            Err _ -> True
            Ok _ -> False
        )
            && Result.toMaybe (Decode.decodeString Recovery.listingDecoder tooMany) == Nothing
      )
    , ( "closing clears backup metadata and ignores late responses"
      , let
            opened =
                Feature.init |> step ctx Feature.OpenRequested |> Tuple.first

            closed =
                opened |> step ctx Feature.CloseRequested |> Tuple.first

            late =
                closed |> step ctx (Feature.Loaded 4 opened.requestId (Err Http.Timeout)) |> Tuple.first
        in
        not closed.open && List.isEmpty closed.backups && late.status == Nothing
      )
    , ( "verification cannot start for unavailable entries"
      , let
            base =
                Feature.init

            model =
                { base | open = True, backups = [] }

            ( after, _ ) =
                step ctx (Feature.VerifyRequested "missing") model
        in
        after.verifying == Nothing && after.receipt == Nothing
      )
    , ( "an accepted backup request is not publication"
      , let
            base =
                Feature.init

            model =
                { base | open = True, configured = True, requestId = 3 }

            ( after, _ ) =
                step ctx (Feature.Created 4 3 (Ok { id = "backup-1", status = "running", error = Nothing })) model
        in
        (after.job |> Maybe.map .status) == Just "running"
            && (after.status |> Maybe.map (String.contains "not complete") |> Maybe.withDefault False)
            && (after.status |> Maybe.map (String.contains "published") |> Maybe.withDefault False) == False
      )
    , ( "closing while backup creation is pending preserves the accepted job for the next open"
      , let
            initial =
                Feature.init

            base =
                { initial | open = True, configured = True }

            ( requesting, _ ) =
                step ctx Feature.CreateRequested base

            closed =
                requesting |> step ctx Feature.CloseRequested |> Tuple.first

            accepted =
                closed
                    |> step ctx (Feature.Created 4 requesting.requestId (Ok { id = "backup-2", status = "running", error = Nothing }))
                    |> Tuple.first

            reopened =
                accepted |> step ctx Feature.OpenRequested |> Tuple.first
        in
        requesting.creating
            && closed.creating
            && closed.requestId == requesting.requestId
            && not accepted.open
            && not accepted.creating
            && (accepted.job |> Maybe.map .id) == Just "backup-2"
            && reopened.open
            && reopened.loading
            && reopened.requestId > accepted.requestId
            && (reopened.job |> Maybe.map .status) == Just "running"
      )
    , ( "reopening during a pending create does not invalidate its response"
      , let
            initial =
                Feature.init

            base =
                { initial | open = True, configured = True }

            ( requesting, _ ) =
                step ctx Feature.CreateRequested base

            closed =
                requesting |> step ctx Feature.CloseRequested |> Tuple.first

            ( reopened, _ ) =
                step ctx Feature.OpenRequested closed

            ( accepted, _ ) =
                step ctx (Feature.Created 4 requesting.requestId (Ok { id = "backup-3", status = "running", error = Nothing })) reopened
        in
        reopened.open
            && reopened.creating
            && reopened.requestId == requesting.requestId
            && not accepted.creating
            && (accepted.job |> Maybe.map .id) == Just "backup-3"
      )
    , ( "publication is the only completed backup state"
      , let
            base =
                Feature.init

            model =
                { base | open = True, configured = True, requestId = 3 }

            ( published, _ ) =
                step ctx (Feature.Created 4 3 (Ok { id = "backup-1", status = "succeeded", error = Nothing })) model

            ( failed, _ ) =
                step ctx (Feature.JobChecked 4 3 (Ok { id = "backup-1", status = "failed", error = Just "database dump failed" })) model

            ( canceled, _ ) =
                step ctx (Feature.JobChecked 4 3 (Ok { id = "backup-1", status = "canceled", error = Nothing })) model
        in
        (published.job |> Maybe.map .status) == Just "succeeded"
            && (published.status |> Maybe.map (String.contains "was published") |> Maybe.withDefault False)
            && (failed.status |> Maybe.map (String.contains "failed before publication") |> Maybe.withDefault False)
            && (failed.status |> Maybe.map (String.contains "database dump failed") |> Maybe.withDefault False)
            && (canceled.status |> Maybe.map (String.contains "canceled before publication") |> Maybe.withDefault False)
      )
    , ( "a second request and a stale response do not become a backup"
      , let
            base =
                Feature.init

            model =
                { base | open = True, configured = True, requestId = 3 }

            ( conflict, outs ) =
                step ctx (Feature.Created 4 3 (Err Api.Recovery.JobConflict)) model

            ( stale, _ ) =
                step ctx (Feature.Created 4 2 (Ok { id = "backup-1", status = "succeeded", error = Nothing })) model

            ( denied, deniedOuts ) =
                step ctx (Feature.Created 4 3 (Err Api.Recovery.JobDenied)) model
        in
        conflict.job == Nothing
            && (conflict.status |> Maybe.map (String.contains "already running") |> Maybe.withDefault False)
            && outs == []
            && stale.job == Nothing
            && denied.job == Nothing
            && deniedOuts == [ Feature.AccessRevoked ]
      )
    , ( "backup job status rejects an unpublished alias"
      , case Decode.decodeString Recovery.jobDecoder """{"id":"backup-1","status":"queued"}""" of
            Err _ ->
                True

            Ok _ ->
                False
      )
    ]
