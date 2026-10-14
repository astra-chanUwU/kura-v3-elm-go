module Api.Import exposing (DuplicateMatch, ImportRequest, ImportResult, batchHashes, duplicatesDecoder, duplicatesUrl, importResultDecoder, lookupDuplicates, requestImport)

{-| Browser folder-import API adapter.

Duplicate lookup is a public read and never carries a credential.
Imports are credentialed multipart posts; the credential is read by the
caller at request time and travels only as an Authorization header.
Hashes are never placed in storage and only appear in the bounded
lookup query the backend contract defines (at most 100 per request).
-}

import Api.Access exposing (authHeaders)
import Domain.Post exposing (PostDetail, detailDecoder)
import File exposing (File)
import Http
import Json.Decode as Decode exposing (Decoder)
import Url.Builder


type alias DuplicateMatch =
    { hash : String
    , postId : String
    }


type alias ImportResult =
    { post : PostDetail
    , duplicate : Bool
    , collectionId : Maybe String
    , attached : Bool
    , attachmentError : Maybe String
    }


type alias ImportRequest =
    { tags : List String
    , source : String
    , artist : String
    , expectedHash : String
    , duplicatePolicy : String
    , collectionId : Maybe String
    }


batchHashes : List String -> List (List String)
batchHashes hashes =
    batch 100 hashes


batch : Int -> List String -> List (List String)
batch size items =
    if size <= 0 || List.isEmpty items then
        []

    else
        List.take size items :: batch size (List.drop size items)


duplicatesUrl : String -> List String -> String
duplicatesUrl apiBase hashes =
    let
        params =
            [ Url.Builder.string "hashes" (String.join "," hashes) ]
    in
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "imports", "duplicates" ] params

    else
        Url.Builder.crossOrigin apiBase [ "api", "imports", "duplicates" ] params


lookupDuplicates : String -> List String -> (Result Http.Error (List DuplicateMatch) -> msg) -> Cmd msg
lookupDuplicates apiBase hashes toMsg =
    Http.get
        { url = duplicatesUrl apiBase hashes
        , expect = Http.expectJson toMsg duplicatesDecoder
        }


duplicatesDecoder : Decoder (List DuplicateMatch)
duplicatesDecoder =
    Decode.field "matches"
        (Decode.list
            (Decode.map2 DuplicateMatch
                (Decode.field "hash" Decode.string)
                (Decode.field "post_id" Decode.string)
            )
        )


importEndpoint : String -> String
importEndpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "imports" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "imports" ] []


requestImport : String -> Maybe String -> File -> ImportRequest -> (Result Http.Error ImportResult -> msg) -> Cmd msg
requestImport apiBase credential file req toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = importEndpoint apiBase
        , body =
            Http.multipartBody
                ([ Http.filePart "file" file
                 , Http.stringPart "expected_hash" req.expectedHash
                 , Http.stringPart "duplicate_policy" req.duplicatePolicy
                 ]
                    ++ List.map (Http.stringPart "tags") req.tags
                    ++ (if String.trim req.source == "" then
                            []

                        else
                            [ Http.stringPart "source" (String.trim req.source) ]
                       )
                    ++ (if String.trim req.artist == "" then
                            []

                        else
                            [ Http.stringPart "artist" (String.trim req.artist) ]
                       )
                    ++ (case req.collectionId of
                            Just id ->
                                if String.trim id == "" then
                                    []

                                else
                                    [ Http.stringPart "collection_id" id ]

                            Nothing ->
                                []
                       )
                )
        , expect = Http.expectJson toMsg importResultDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


importResultDecoder : Decoder ImportResult
importResultDecoder =
    Decode.map5
        (\post duplicate collectionId attached attachmentError ->
            { post = post
            , duplicate = duplicate
            , collectionId = collectionId
            , attached = attached
            , attachmentError = attachmentError
            }
        )
        (Decode.field "post" detailDecoder)
        (Decode.field "duplicate" Decode.bool)
        (Decode.field "collection_id" (Decode.nullable Decode.string))
        (Decode.field "attached" Decode.bool)
        (Decode.oneOf
            [ Decode.field "attachment_error" (Decode.nullable Decode.string)
            , Decode.succeed Nothing
            ]
        )
