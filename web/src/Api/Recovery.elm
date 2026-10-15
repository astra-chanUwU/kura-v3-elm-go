module Api.Recovery exposing (JobFailure(..), cancel, create, job, list, verify)

import Api.Access exposing (authHeaders)
import Domain.Recovery exposing (Job, Listing, Receipt, jobDecoder, listingDecoder, receiptDecoder)
import Http
import Json.Decode as Decode
import String
import Url.Builder


list : String -> Maybe String -> (Result Http.Error Listing -> msg) -> Cmd msg
list apiBase credential toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "recovery", "backups" ]
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg listingDecoder
        , timeout = Just 15000
        , tracker = Nothing
        }


type JobFailure
    = JobDenied
    | JobUnavailable String
    | JobConflict
    | JobMissing
    | JobRejected String


create : String -> Maybe String -> (Result JobFailure Job -> msg) -> Cmd msg
create apiBase credential toMsg =
    jobRequest "POST" apiBase credential [ "api", "recovery", "backups" ] toMsg


job : String -> Maybe String -> String -> (Result JobFailure Job -> msg) -> Cmd msg
job apiBase credential id toMsg =
    jobRequest "GET" apiBase credential [ "api", "recovery", "backups", id ] toMsg


cancel : String -> Maybe String -> String -> (Result JobFailure Job -> msg) -> Cmd msg
cancel apiBase credential id toMsg =
    jobRequest "DELETE" apiBase credential [ "api", "recovery", "backups", id ] toMsg


jobRequest : String -> String -> Maybe String -> List String -> (Result JobFailure Job -> msg) -> Cmd msg
jobRequest method apiBase credential path toMsg =
    Http.request
        { method = method
        , headers = authHeaders credential
        , url = endpoint apiBase path
        , body = Http.emptyBody
        , expect = expectJob toMsg
        , timeout = Just 15000
        , tracker = Nothing
        }


expectJob : (Result JobFailure Job -> msg) -> Http.Expect msg
expectJob toMsg =
    Http.expectStringResponse toMsg <|
        \response ->
            case response of
                Http.BadUrl_ _ ->
                    Err (JobRejected "request")

                Http.Timeout_ ->
                    Err (JobRejected "timeout")

                Http.NetworkError_ ->
                    Err (JobRejected "network")

                Http.BadStatus_ meta body ->
                    Err (failureFromStatus meta.statusCode body)

                Http.GoodStatus_ _ body ->
                    case Decode.decodeString jobDecoder body of
                        Ok value ->
                            Ok value

                        Err _ ->
                            Err (JobRejected "response")


failureFromStatus : Int -> String -> JobFailure
failureFromStatus status body =
    if status == 401 || status == 403 then
        JobDenied

    else if status == 409 then
        JobConflict

    else if status == 404 then
        JobMissing

    else if status == 503 then
        JobUnavailable (knownUnavailable body)

    else
        JobRejected (String.fromInt status)


knownUnavailable : String -> String
knownUnavailable body =
    case Decode.decodeString (Decode.field "error" Decode.string) body of
        Ok "backup root is not configured" ->
            "backup root is not configured"

        Ok "S3 backups are not supported" ->
            "S3 backups are not supported"

        Ok "recovery unavailable" ->
            "recovery unavailable"

        _ ->
            "backup creation unavailable"


verify : String -> Maybe String -> String -> (Result Http.Error Receipt -> msg) -> Cmd msg
verify apiBase credential id toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "recovery", "backups", id, "verify" ]
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg receiptDecoder
        , timeout = Just 120000
        , tracker = Nothing
        }


endpoint : String -> List String -> String
endpoint apiBase path =
    if String.trim apiBase == "" then
        Url.Builder.absolute path []

    else
        Url.Builder.crossOrigin apiBase path []
