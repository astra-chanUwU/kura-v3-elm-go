module Api.Post exposing (search, mediaUrl)

import Domain.Post exposing (SearchResponse, responseDecoder)
import Http
import String
import Url.Builder


search : String -> String -> (Result Http.Error SearchResponse -> msg) -> Cmd msg
search apiBase query toMsg =
    Http.get
        { url = endpoint apiBase query
        , expect = Http.expectJson toMsg responseDecoder
        }


endpoint : String -> String -> String
endpoint apiBase query =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts" ] [ Url.Builder.string "q" query ]

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts" ] [ Url.Builder.string "q" query ]


mediaUrl : String -> String -> String
mediaUrl apiBase value =
    if isAbsolute value || String.trim apiBase == "" then
        value

    else if String.startsWith "/" value then
        trimTrailingSlash apiBase ++ value

    else
        trimTrailingSlash apiBase ++ "/" ++ value


isAbsolute : String -> Bool
isAbsolute value =
    List.any (\prefix -> String.startsWith prefix value) [ "http://", "https://", "data:", "blob:" ]


trimTrailingSlash : String -> String
trimTrailingSlash value =
    if String.endsWith "/" value then
        String.dropRight 1 value

    else
        value
