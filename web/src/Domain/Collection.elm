module Domain.Collection exposing
    ( Collection
    , CollectionAck
    , CollectionPostsPage
    , CollectionSummary
    , CreateResponse
    , ManageResult
    , MoveResult
    , PostsResponse
    , ackDecoder
    , decoder
    , manageDecoder
    , moveDecoder
    , postsDecoder
    , responseDecoder
    , summariesDecoder
    , summaryDecoder
    )

import Domain.Post exposing (PostSummary, SearchResponse)
import Json.Decode as Decode exposing (Decoder)


{-| Legacy full collection shape. Kept only so `Api.Collection.list` and
`Api.Collection.reorder` stay source-compatible for external callers; the
library UI never stores this unbounded member list.
-}
type alias Collection =
    { id : String
    , name : String
    , postIds : List String
    }


type alias CreateResponse =
    Collection


type alias PostsResponse =
    SearchResponse


{-| Compact navigation metadata for one collection: counts only, never
member IDs. `version` is the membership/order version that version-checked
moves and rename/delete requests must echo back; zero is a real version
for a freshly created collection.
-}
type alias CollectionSummary =
    { id : String
    , name : String
    , version : Int
    , memberCount : Int
    , visiblePostCount : Int
    }


{-| Compact acknowledgement for create/add mutations. The legacy server
payload may also carry `post_ids`; the UI deliberately decodes only the
bounded identity fields and never keeps the member array.
-}
type alias CollectionAck =
    { id : String
    , name : String
    , version : Int
    }


{-| Compact result of a version-checked single-post move. Both `id`
and `collectionId` are the collection ID (the server sets
`id = collection_id = <collection>`); the moved post is never echoed.
A response for the wrong collection can never read as success.
-}
type alias MoveResult =
    { id : String
    , collectionId : String
    , version : Int
    , changed : Bool
    }


{-| Compact result of a collection rename or delete. Delete carries the
removed collection's last name and version; it never implies posts were
removed.
-}
type alias ManageResult =
    { id : String
    , name : String
    , version : Int
    , changed : Bool
    }


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


summaryDecoder : Decoder CollectionSummary
summaryDecoder =
    Decode.map5 CollectionSummary
        (Decode.field "id" nonEmptyString)
        (Decode.field "name" nonEmptyString)
        (Decode.field "version" nonNegativeInt)
        (Decode.field "member_count" nonNegativeInt)
        (Decode.field "visible_post_count" nonNegativeInt)


summariesDecoder : Decoder (List CollectionSummary)
summariesDecoder =
    Decode.field "collections" (Decode.list summaryDecoder)


ackDecoder : Decoder CollectionAck
ackDecoder =
    Decode.map3 CollectionAck
        (Decode.field "id" nonEmptyString)
        (Decode.field "name" nonEmptyString)
        (Decode.field "version" nonNegativeInt)


moveDecoder : Decoder MoveResult
moveDecoder =
    Decode.map4 MoveResult
        (Decode.field "id" nonEmptyString)
        (Decode.field "collection_id" nonEmptyString)
        (Decode.field "version" nonNegativeInt)
        (Decode.field "changed" Decode.bool)


manageDecoder : Decoder ManageResult
manageDecoder =
    Decode.map4 ManageResult
        (Decode.field "id" nonEmptyString)
        (Decode.field "name" nonEmptyString)
        (Decode.field "version" nonNegativeInt)
        (Decode.field "changed" Decode.bool)


nonEmptyString : Decoder String
nonEmptyString =
    Decode.string
        |> Decode.andThen
            (\value ->
                if String.trim value == "" then
                    Decode.fail "expected a non-empty string"

                else
                    Decode.succeed value
            )


nonNegativeInt : Decoder Int
nonNegativeInt =
    Decode.int
        |> Decode.andThen
            (\value ->
                if value < 0 then
                    Decode.fail "expected a non-negative integer"

                else
                    Decode.succeed value
            )
