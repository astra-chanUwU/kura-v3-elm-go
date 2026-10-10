module Domain.Collection exposing (Collection, CollectionPostsPage, CreateResponse, PostsResponse, decoder, postsDecoder, responseDecoder)

import Domain.Post exposing (PostSummary, SearchResponse)
import Json.Decode as Decode exposing (Decoder)


type alias Collection =
    { id : String
    , name : String
    , postIds : List String
    }


type alias CreateResponse =
    Collection


type alias PostsResponse =
    SearchResponse


{-| One versioned page of collection members. `collectionVersion` is the
membership/order version observed by the same snapshot that produced the
page; the opaque `nextCursor` echoes it back so concurrent changes
surface as conflicts instead of silent gaps or duplicates.
-}
type alias CollectionPostsPage =
    { posts : List PostSummary
    , nextCursor : Maybe String
    , collectionVersion : Int
    }


{-| Additive decoder: older payloads without `collection_version` still
decode (version 0) so additive server fields never break this client.
-}
postsDecoder : Decoder CollectionPostsPage
postsDecoder =
    Decode.map3 CollectionPostsPage
        (Decode.field "posts" (Decode.list Domain.Post.decoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))
        (Decode.oneOf [ Decode.field "collection_version" Decode.int, Decode.succeed 0 ])


decoder : Decoder Collection
decoder =
    Decode.map3 Collection
        (Decode.field "id" Decode.string)
        (Decode.field "name" Decode.string)
        (Decode.oneOf [ Decode.field "post_ids" (Decode.list Decode.string), Decode.succeed [] ])


responseDecoder : Decoder (List Collection)
responseDecoder =
    Decode.field "collections" (Decode.list decoder)
