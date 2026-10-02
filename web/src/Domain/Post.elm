module Domain.Post exposing (PostDetail, PostSummary, SearchResponse, decoder, detailDecoder, responseDecoder)

import Json.Decode as Decode exposing (Decoder)


type alias PostSummary =
    { id : String
    , previewUrl : String
    , originalUrl : String
    , mediaType : String
    , width : Int
    , height : Int
    , tags : List String
    }


type alias SearchResponse =
    { posts : List PostSummary
    , nextCursor : Maybe String
    }


type alias PostDetail =
    { id : String
    , previewUrl : String
    , originalUrl : String
    , mediaType : String
    , width : Int
    , height : Int
    , source : String
    , artist : String
    , hash : String
    , fileSize : Int
    , createdAt : String
    , tags : List String
    }


detailDecoder : Decoder PostDetail
detailDecoder =
    Decode.map5
        (\base hash fileSize createdAt tags ->
            { base
                | hash = hash
                , fileSize = fileSize
                , createdAt = createdAt
                , tags = tags
            }
        )
        (Decode.map8
            (\id previewUrl originalUrl mediaType width height source artist ->
                { id = id
                , previewUrl = previewUrl
                , originalUrl = originalUrl
                , mediaType = mediaType
                , width = width
                , height = height
                , source = source
                , artist = artist
                , hash = ""
                , fileSize = 0
                , createdAt = ""
                , tags = []
                }
            )
            (Decode.field "id" Decode.string)
            (Decode.field "preview_url" Decode.string)
            (Decode.field "original_url" Decode.string)
            (Decode.field "media_type" Decode.string)
            (Decode.field "width" Decode.int)
            (Decode.field "height" Decode.int)
            (Decode.field "source" Decode.string)
            (Decode.field "artist" Decode.string)
        )
        (Decode.field "hash" Decode.string)
        (Decode.field "file_size" Decode.int)
        (Decode.field "created_at" Decode.string)
        (Decode.field "tags" (Decode.list Decode.string))


decoder : Decoder PostSummary
decoder =
    Decode.map7 PostSummary
        (Decode.field "id" Decode.string)
        (Decode.field "preview_url" Decode.string)
        (Decode.field "original_url" Decode.string)
        (Decode.field "media_type" Decode.string)
        (Decode.field "width" Decode.int)
        (Decode.field "height" Decode.int)
        (Decode.oneOf [ Decode.field "tags" (Decode.list Decode.string), Decode.succeed [] ])


responseDecoder : Decoder SearchResponse
responseDecoder =
    Decode.map2 SearchResponse
        (Decode.field "posts" (Decode.list decoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))
