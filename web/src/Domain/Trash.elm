module Domain.Trash exposing (TrashPost, TrashPage, TrashResult, pageDecoder, resultDecoder, responseDecoder)

import Json.Decode as Decode exposing (Decoder)


{-| Metadata-only trash row. No preview/original URLs are decoded:
public media correctly 404s while trashed, so the UI never requests them.
-}
type alias TrashPost =
    { id : String
    , mediaType : String
    , width : Int
    , height : Int
    , tags : List String
    , deletedAt : String
    , moderationState : String
    }


type alias TrashPage =
    { posts : List TrashPost
    , nextCursor : Maybe String
    }


type alias TrashResult =
    { id : String
    , changed : Bool
    }


postDecoder : Decoder TrashPost
postDecoder =
    Decode.map7 TrashPost
        (Decode.field "id" Decode.string)
        (Decode.field "media_type" Decode.string)
        (Decode.field "width" Decode.int)
        (Decode.field "height" Decode.int)
        (Decode.oneOf [ Decode.field "tags" (Decode.list Decode.string), Decode.succeed [] ])
        (Decode.oneOf [ Decode.field "deleted_at" Decode.string, Decode.succeed "" ])
        (Decode.oneOf [ Decode.field "moderation_state" Decode.string, Decode.succeed "" ])


pageDecoder : Decoder TrashPage
pageDecoder =
    Decode.map2 TrashPage
        (Decode.field "posts" (Decode.list postDecoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))


resultDecoder : Decoder TrashResult
resultDecoder =
    Decode.map2 TrashResult (Decode.field "id" Decode.string) (Decode.field "changed" Decode.bool)


responseDecoder : Decoder (List TrashResult)
responseDecoder =
    Decode.field "posts" (Decode.list resultDecoder)
