module Api.Upload exposing (Metadata, endpoint, maxBytes, request, uploadErrorToString)

{-| Single-file upload against `POST /api/uploads`.

The request is built with a caller-supplied tracker key so the queue can
follow real transfer progress through `Http.track`. The response body is the
created `PostDetail`, which carries the thumbnail job id used for separate
thumbnail polling. Requests are never retried here: after a lost response
the post may already exist, so resubmission stays an explicit user decision.
-}

import Api.Access exposing (authHeaders)
import Domain.Post exposing (PostDetail, detailDecoder)
import File exposing (File)
import Http
import Url.Builder


{-| Server-side bound: images must be between 1 byte and 25 MiB.
-}
maxBytes : Int
maxBytes =
    25 * 1024 * 1024


type alias Metadata =
    { tags : List String
    , source : String
    , artist : String
    }


endpoint : String -> String
endpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "uploads" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "uploads" ] []


multipartBody : File -> Metadata -> Http.Body
multipartBody file metadata =
    Http.multipartBody
        ([ Http.filePart "file" file ]
            ++ List.map (Http.stringPart "tags") metadata.tags
            ++ (if String.trim metadata.source == "" then
                    []

                else
                    [ Http.stringPart "source" (String.trim metadata.source) ]
               )
            ++ (if String.trim metadata.artist == "" then
                    []

                else
                    [ Http.stringPart "artist" (String.trim metadata.artist) ]
               )
        )


{-| Upload one file with transfer progress visible under `tracker`. The
unlocked credential travels as an `Authorization` header, never in the
multipart body or the URL.
-}
request : String -> Maybe String -> String -> File -> Metadata -> (Result Http.Error PostDetail -> msg) -> Cmd msg
request apiBase credential tracker file metadata toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase
        , body = multipartBody file metadata
        , expect = Http.expectJson toMsg detailDecoder
        , timeout = Nothing
        , tracker = Just tracker
        }


{-| Honest, actionable messages. Timeouts and network errors say the post
may already exist so the caller never resubmits blindly.
-}
uploadErrorToString : Http.Error -> String
uploadErrorToString error =
    case error of
        Http.BadUrl url ->
            "Invalid API URL: " ++ url

        Http.Timeout ->
            "The server stopped responding while saving. The post may already exist — check the Library before uploading again."

        Http.NetworkError ->
            "The connection was lost while saving. The post may already exist — check the Library before uploading again."

        Http.BadStatus 400 ->
            "The server rejected the file. It may not be a supported JPEG, PNG, or GIF image."

        Http.BadStatus 401 ->
            "Authorization failed. A write capability is required to upload."

        Http.BadStatus 403 ->
            "Authorization failed. This credential cannot upload."

        Http.BadStatus 413 ->
            "The server rejected the upload: the file exceeds 25 MiB."

        Http.BadStatus 422 ->
            "The server could not process the file as an image."

        Http.BadStatus status ->
            "Saving failed (HTTP " ++ String.fromInt status ++ "). The post was not confirmed saved."

        Http.BadBody _ ->
            "The server confirmed the upload with an unreadable response. Check the Library — the post may already exist."
