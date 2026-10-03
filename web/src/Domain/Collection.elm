module Domain.Collection exposing (Collection, CreateResponse, decoder, responseDecoder)

import Json.Decode as Decode exposing (Decoder)


type alias Collection =
    { id : String
    , name : String
    , postIds : List String
    }


type alias CreateResponse =
    Collection


decoder : Decoder Collection
decoder =
    Decode.map3 Collection
        (Decode.field "id" Decode.string)
        (Decode.field "name" Decode.string)
        (Decode.oneOf [ Decode.field "post_ids" (Decode.list Decode.string), Decode.succeed [] ])


responseDecoder : Decoder (List Collection)
responseDecoder =
    Decode.field "collections" (Decode.list decoder)
