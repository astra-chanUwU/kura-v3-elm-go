module RecoveryTests exposing (suite)

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
    ]
