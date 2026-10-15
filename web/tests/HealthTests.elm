module HealthTests exposing (suite)

import Domain.Health as Health
import Feature.Health as Feature
import Http
import Json.Decode as Decode


ctx : Feature.Context
ctx =
    Feature.context "" (Just "secret") True 4


step : Feature.Msg -> Feature.Model -> ( Feature.Model, List Feature.OutMsg )
step msg model =
    let
        ( next, _, outs ) =
            Feature.update ctx msg model
    in
    ( next, outs )


summaryJSON : String
summaryJSON =
    """{"missing_originals":{"known":false,"value":0},"missing_thumbnails":{"known":true,"value":2},"failed_thumbnails":{"known":true,"value":0},"processing_backlog":{"known":true,"value":1},"storage_bytes":{"known":false,"value":0},"latest_verified_backup":{"id":"nightly-1","created_at":"2026-10-15T12:00:00Z"},"media":"local"}"""


suite : List ( String, Bool )
suite =
    [ ( "library health leaves unscanned storage unknown"
      , case Decode.decodeString Health.summaryDecoder summaryJSON of
            Ok summary ->
                not summary.missingOriginals.known
                    && summary.missingThumbnails.value == 2
                    && not summary.storageBytes.known
                    && (summary.latestVerifiedBackup |> Maybe.map .id) == Just "nightly-1"

            Err _ ->
                False
      )
    , ( "library health hides a scan error that is not a known phrase"
      , Feature.safeScanNote "C:/library/uploads/secret.png" == Nothing
            && Feature.safeScanNote "media check failed" == Just "media check failed"
            && Feature.countLabel False 4 == "unknown"
      )
    , ( "a viewer cannot open library health"
      , let
            ( next, _, outs ) =
                Feature.update { ctx | canAdmin = False } Feature.OpenRequested Feature.init
        in
        not next.open && outs == [] && next.status == Just "Admin access is required to view library health."
      )
    , ( "an in-flight thumbnail repair is not sent twice"
      , let
            initial =
                Feature.init

            open =
                { initial | open = True, thumbnails = [ "9" ], requestId = 3 }

            ( first, _ ) =
                step (Feature.RepairRequested "9") open

            ( second, _ ) =
                step (Feature.RepairRequested "9") first
        in
        first.repairing && first.requestId == 4 && second.requestId == 4
      )
    , ( "a stale repair response does not replace the current request"
      , let
            initial =
                Feature.init

            model =
                { initial | open = True, repairing = True, requestId = 5, status = Just "current" }

            ( next, _ ) =
                step (Feature.Repaired 4 4 (Ok { jobId = "job-1", status = "pending" })) model
        in
        next.repairing && next.status == Just "current"
      )
    , ( "a denied reload clears the panel"
      , let
            ( next, outs ) =
                let
                    initial =
                        Feature.init
                in
                Feature.update { ctx | canAdmin = False } Feature.ReloadRequested { initial | open = True } |> \( model, _, outMsgs ) -> ( model, outMsgs )
        in
        not next.open && outs == [ Feature.AccessRevoked ]
      )
    , ( "s3 checks stay unsupported without a storage size"
      , case Decode.decodeString Health.summaryDecoder """{"missing_originals":{"known":false,"value":0},"missing_thumbnails":{"known":true,"value":0},"failed_thumbnails":{"known":true,"value":0},"processing_backlog":{"known":true,"value":0},"storage_bytes":{"known":false,"value":0},"latest_verified_backup":null,"media":"s3","s3_checks":"unsupported"}""" of
            Ok summary ->
                summary.s3Checks == "unsupported" && not summary.storageBytes.known && summary.latestVerifiedBackup == Nothing

            Err _ ->
                False
      )
    ]
