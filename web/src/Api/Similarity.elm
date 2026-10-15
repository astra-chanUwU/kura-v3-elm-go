module Api.Similarity exposing
    ( getStatus
    , indexPosts
    , listSimilar
    , similarEndpoint
    , statusEndpoint
    )

{-| Near-copy review transport.

The similar read is a public GET (no auto-indexing, works while locked).
The index POST and status GET are protected: they carry the unlocked
credential through the shared `authHeaders` helper (cookie same-origin
auth also applies). Every request times out after 30s; an uncertain POST
result is surfaced to the caller and never retried here.
-}

import Api.Access exposing (authHeaders)
import Domain.Similarity as Similarity exposing (IndexResponse, SimilarPage, StatusPage)
import Http
import Json.Encode as Encode
import Url
import Url.Builder


listSimilar : String -> String -> Int -> Int -> Maybe String -> (Result Http.Error SimilarPage -> msg) -> Cmd msg
listSimilar apiBase anchorId distance limit cursor toMsg =
    Http.request
        { method = "GET"
        , headers = []
        , body = Http.emptyBody
        , timeout = Just 30000
        , tracker = Nothing
        , url = similarEndpoint apiBase anchorId distance limit cursor
        , expect = Http.expectJson toMsg Similarity.similarPageDecoder
        }


indexPosts : String -> Maybe String -> List String -> (Result Http.Error IndexResponse -> msg) -> Cmd msg
indexPosts apiBase credential postIds toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , body = Http.jsonBody (Encode.object [ ( "post_ids", Encode.list Encode.string postIds ) ])
        , timeout = Just 30000
        , tracker = Nothing
        , url = endpoint apiBase [ "api", "posts", "similarity", "index" ]
        , expect = Http.expectJson toMsg Similarity.indexResponseDecoder
        }


getStatus : String -> Maybe String -> Int -> Maybe String -> (Result Http.Error StatusPage -> msg) -> Cmd msg
getStatus apiBase credential limit cursor toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , body = Http.emptyBody
        , timeout = Just 30000
        , tracker = Nothing
        , url = statusEndpoint apiBase limit cursor
        , expect = Http.expectJson toMsg Similarity.statusPageDecoder
        }


similarEndpoint : String -> String -> Int -> Int -> Maybe String -> String
similarEndpoint apiBase anchorId distance limit cursor =
    let
        query =
            "distance="
                ++ String.fromInt distance
                ++ "&limit="
                ++ String.fromInt limit
                ++ (case cursor of
                        Just value ->
                            "&cursor=" ++ Url.percentEncode value

                        Nothing ->
                            ""
                   )
    in
    base apiBase ++ "/api/posts/" ++ Url.percentEncode anchorId ++ "/similar?" ++ query


statusEndpoint : String -> Int -> Maybe String -> String
statusEndpoint apiBase limit cursor =
    let
        params =
            Url.Builder.int "limit" limit
                :: (case cursor of
                        Just value ->
                            [ Url.Builder.string "cursor" value ]

                        Nothing ->
                            []
                   )
    in
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "similarity", "status" ] params

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "similarity", "status" ] params


endpoint : String -> List String -> String
endpoint apiBase segments =
    if String.trim apiBase == "" then
        Url.Builder.absolute segments []

    else
        Url.Builder.crossOrigin apiBase segments []


base : String -> String
base apiBase =
    let
        trimmed =
            String.trim apiBase
    in
    if String.endsWith "/" trimmed then
        String.dropRight 1 trimmed

    else
        trimmed
