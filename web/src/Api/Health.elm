module Api.Health exposing (cancelScan, repair, scan, startScan, summary, thumbnails)

import Api.Access exposing (authHeaders)
import Domain.Health exposing (Page, Repair, Scan, Summary, pageDecoder, repairDecoder, scanDecoder, summaryDecoder)
import Http
import Json.Encode as Encode
import Url.Builder


summary : String -> Maybe String -> (Result Http.Error Summary -> msg) -> Cmd msg
summary apiBase credential toMsg =
    get apiBase credential [ "api", "health", "library" ] (Http.expectJson toMsg summaryDecoder)


scan : String -> Maybe String -> (Result Http.Error Scan -> msg) -> Cmd msg
scan apiBase credential toMsg =
    get apiBase credential [ "api", "health", "library", "scan" ] (Http.expectJson toMsg scanDecoder)


thumbnails : String -> Maybe String -> (Result Http.Error Page -> msg) -> Cmd msg
thumbnails apiBase credential toMsg =
    get apiBase credential [ "api", "health", "library", "thumbnails" ] (Http.expectJson toMsg pageDecoder)


startScan : String -> Maybe String -> (Result Http.Error Scan -> msg) -> Cmd msg
startScan apiBase credential toMsg =
    send "POST" apiBase credential [ "api", "health", "library", "scan" ] Http.emptyBody (Http.expectJson toMsg scanDecoder)


cancelScan : String -> Maybe String -> (Result Http.Error Scan -> msg) -> Cmd msg
cancelScan apiBase credential toMsg =
    send "DELETE" apiBase credential [ "api", "health", "library", "scan" ] Http.emptyBody (Http.expectJson toMsg scanDecoder)


repair : String -> Maybe String -> String -> (Result Http.Error Repair -> msg) -> Cmd msg
repair apiBase credential postId toMsg =
    send "POST"
        apiBase
        credential
        [ "api", "health", "library", "repairs" ]
        (Http.jsonBody (Encode.object [ ( "post_id", Encode.string postId ) ]))
        (Http.expectJson toMsg repairDecoder)


get : String -> Maybe String -> List String -> Http.Expect msg -> Cmd msg
get apiBase credential path expect =
    send "GET" apiBase credential path Http.emptyBody expect


send : String -> String -> Maybe String -> List String -> Http.Body -> Http.Expect msg -> Cmd msg
send method apiBase credential path body expect =
    Http.request
        { method = method
        , headers = authHeaders credential
        , url = endpoint apiBase path
        , body = body
        , expect = expect
        , timeout = Just 15000
        , tracker = Nothing
        }


endpoint : String -> List String -> String
endpoint apiBase path =
    if String.trim apiBase == "" then
        Url.Builder.absolute path []

    else
        Url.Builder.crossOrigin apiBase path []
