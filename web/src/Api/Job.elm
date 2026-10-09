module Api.Job exposing (Job, decoder, endpoint, get, isTerminal, jobErrorToString)

{-| Status polling for durable jobs at `GET /api/jobs/{id}`.

Uploads use this to follow thumbnail processing after the original post is
saved. Polling is bounded by the caller: requests stop at terminal states
(`succeeded`, `failed`, `cancelled`), and transport errors never restart a
finished upload.
-}

import Http
import Json.Decode as Decode exposing (Decoder)
import Url.Builder


type alias Job =
    { id : String
    , kind : String
    , status : String
    , progress : Int
    , lastError : String
    }


decoder : Decoder Job
decoder =
    Decode.map5 Job
        (Decode.field "id" Decode.string)
        (Decode.oneOf [ Decode.field "kind" Decode.string, Decode.succeed "" ])
        (Decode.oneOf [ Decode.field "status" Decode.string, Decode.succeed "" ])
        (Decode.oneOf [ Decode.field "progress" Decode.int, Decode.succeed 0 ])
        (Decode.oneOf [ Decode.field "last_error" Decode.string, Decode.succeed "" ])


{-| Terminal statuses end the job lifecycle; anything else keeps polling.
-}
isTerminal : String -> Bool
isTerminal status =
    case status of
        "succeeded" ->
            True

        "failed" ->
            True

        "cancelled" ->
            True

        _ ->
            False


endpoint : String -> String -> String
endpoint apiBase jobId =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "jobs", jobId ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "jobs", jobId ] []


get : String -> String -> (Result Http.Error Job -> msg) -> Cmd msg
get apiBase jobId toMsg =
    Http.get
        { url = endpoint apiBase jobId
        , expect = Http.expectJson toMsg decoder
        }


jobErrorToString : Http.Error -> String
jobErrorToString error =
    case error of
        Http.BadUrl url ->
            "Invalid API URL: " ++ url

        Http.Timeout ->
            "Thumbnail status timed out. The original was saved; its preview will update on reload."

        Http.NetworkError ->
            "Thumbnail status is unreachable. The original was saved; its preview will update on reload."

        Http.BadStatus 404 ->
            "The thumbnail job is unknown. The original was saved."

        Http.BadStatus status ->
            "Thumbnail status failed (HTTP " ++ String.fromInt status ++ "). The original was saved."

        Http.BadBody _ ->
            "Thumbnail status was unreadable. The original was saved."
