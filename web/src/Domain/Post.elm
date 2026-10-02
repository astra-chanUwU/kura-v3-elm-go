module Domain.Post exposing (PostSummary, SearchResponse, decoder, responseDecoder)

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
