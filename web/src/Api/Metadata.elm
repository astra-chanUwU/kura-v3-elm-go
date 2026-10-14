module Api.Metadata exposing (endpoint, encodeReadRequest, encodeRequest, edit, read, readEndpoint)

{-| Versioned source/artist editing against `POST /api/posts/metadata`
and the compact bulk snapshot read at `POST /api/posts/metadata/read`.

Omitted fields stay unchanged on the server; a present empty string
clears the field. The target version is always encoded explicitly,
including zero. The credential travels only as an Authorization header.
-}

import Api.Access exposing (authHeaders)
import Domain.Post exposing (MetadataReadResponse, MetadataResponse, MetadataTarget, metadataReadResponseDecoder, metadataResponseDecoder)
import Http
import Json.Encode as Encode
import Url.Builder


endpoint : String -> String
endpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "metadata" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "metadata" ] []


readEndpoint : String -> String
readEndpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "metadata", "read" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "metadata", "read" ] []


encodeTarget : MetadataTarget -> Encode.Value
encodeTarget target =
    Encode.object
        [ ( "id", Encode.string target.id )
        , ( "version", Encode.int target.version )
        ]


encodeRequest : List MetadataTarget -> Maybe String -> Maybe String -> Encode.Value
encodeRequest targets source artist =
    Encode.object
        ([ ( "posts", Encode.list encodeTarget targets ) ]
            ++ (case source of
                    Just value ->
                        [ ( "source", Encode.string value ) ]

                    Nothing ->
                        []
               )
            ++ (case artist of
                    Just value ->
                        [ ( "artist", Encode.string value ) ]

                    Nothing ->
                        []
               )
        )


encodeReadRequest : List String -> Encode.Value
encodeReadRequest postIds =
    Encode.object [ ( "post_ids", Encode.list Encode.string postIds ) ]


edit : String -> Maybe String -> List MetadataTarget -> Maybe String -> Maybe String -> (Result Http.Error MetadataResponse -> msg) -> Cmd msg
edit apiBase credential targets source artist toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase
        , body = Http.jsonBody (encodeRequest targets source artist)
        , expect = Http.expectJson toMsg metadataResponseDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }


read : String -> Maybe String -> List String -> (Result Http.Error MetadataReadResponse -> msg) -> Cmd msg
read apiBase credential postIds toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = readEndpoint apiBase
        , body = Http.jsonBody (encodeReadRequest postIds)
        , expect = Http.expectJson toMsg metadataReadResponseDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }
