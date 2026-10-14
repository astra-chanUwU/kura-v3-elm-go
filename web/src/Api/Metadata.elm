module Api.Metadata exposing (endpoint, encodeRequest, edit)

{-| Versioned source/artist editing against `POST /api/posts/metadata`.

Omitted fields stay unchanged on the server; a present empty string
clears the field. The target version is always encoded explicitly,
including zero. The credential travels only as an Authorization header.
-}

import Api.Access exposing (authHeaders)
import Domain.Post exposing (MetadataResponse, MetadataTarget, metadataResponseDecoder)
import Http
import Json.Encode as Encode
import Url.Builder


endpoint : String -> String
endpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "metadata" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "metadata" ] []


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


edit : String -> Maybe String -> List MetadataTarget -> Maybe String -> Maybe String -> (Result Http.Error MetadataResponse -> msg) -> Cmd msg
edit apiBase credential targets source artist toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase
        , body = Http.jsonBody (encodeRequest targets source artist)
        , expect = Http.expectJson toMsg metadataResponseDecoder
        , timeout = Nothing
        , tracker = Nothing
        }
