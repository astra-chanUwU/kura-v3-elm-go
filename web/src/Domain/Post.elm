module Domain.Post exposing (PostDetail, PostSummary, SearchResponse, TagEditResponse, TagEditResult, TagEditTarget, TagRevision, ReactionResponse, ReactionResult, ReactionTarget, ReactionRevision, decoder, detailDecoder, responseDecoder, tagEditResponseDecoder, reactionResponseDecoder)

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
    , tagVersion : Int
    , favorite : Bool
    , score : Int
    , reactionVersion : Int
    , history : List TagRevision
    , reactionHistory : List ReactionRevision
    }


type alias TagRevision =
    { version : Int
    , kind : String
    , addedTags : List String
    , removedTags : List String
    , createdAt : String
    }


type alias TagEditTarget =
    { id : String
    , version : Int
    }


type alias TagEditResult =
    { id : String
    , version : Int
    , tags : List String
    , changed : Bool
    }


type alias TagEditResponse =
    { posts : List TagEditResult }


type alias ReactionTarget =
    { id : String
    , version : Int
    }


type alias ReactionResult =
    { id : String
    , version : Int
    , favorite : Bool
    , score : Int
    , changed : Bool
    }


type alias ReactionResponse =
    { posts : List ReactionResult }


type alias ReactionRevision =
    { version : Int
    , favorite : Bool
    , score : Int
    , createdAt : String
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
                , tagVersion = 0
                , favorite = False
                , score = 0
                , reactionVersion = 0
                , history = []
                , reactionHistory = []
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
        |> Decode.andThen
            (\detail ->
                Decode.map6
                    (\tagVersion history favorite score reactionVersion reactionHistory ->
                        { detail
                            | tagVersion = tagVersion
                            , history = history
                            , favorite = favorite
                            , score = score
                            , reactionVersion = reactionVersion
                            , reactionHistory = reactionHistory
                        }
                    )
                    (Decode.oneOf [ Decode.field "tag_version" Decode.int, Decode.succeed 0 ])
                    (Decode.oneOf [ Decode.field "history" (Decode.list revisionDecoder), Decode.succeed [] ])
                    (Decode.oneOf [ Decode.field "favorite" Decode.bool, Decode.succeed False ])
                    (Decode.oneOf [ Decode.field "score" Decode.int, Decode.succeed 0 ])
                    (Decode.oneOf [ Decode.field "reaction_version" Decode.int, Decode.succeed 0 ])
                    (Decode.oneOf [ Decode.field "reaction_history" (Decode.list reactionRevisionDecoder), Decode.succeed [] ])
            )


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


revisionDecoder : Decoder TagRevision
revisionDecoder =
    Decode.map5 TagRevision
        (Decode.field "version" Decode.int)
        (Decode.field "kind" Decode.string)
        (Decode.field "added_tags" (Decode.list Decode.string))
        (Decode.field "removed_tags" (Decode.list Decode.string))
        (Decode.field "created_at" Decode.string)


tagEditResponseDecoder : Decoder TagEditResponse
tagEditResponseDecoder =
    Decode.map (TagEditResponse) (Decode.field "posts" (Decode.list tagEditResultDecoder))


tagEditResultDecoder : Decoder TagEditResult
tagEditResultDecoder =
    Decode.map4 TagEditResult
        (Decode.field "id" Decode.string)
        (Decode.field "version" Decode.int)
        (Decode.field "tags" (Decode.list Decode.string))
        (Decode.field "changed" Decode.bool)


reactionResponseDecoder : Decoder ReactionResponse
reactionResponseDecoder =
    Decode.map ReactionResponse (Decode.field "posts" (Decode.list reactionResultDecoder))


reactionResultDecoder : Decoder ReactionResult
reactionResultDecoder =
    Decode.map5 ReactionResult
        (Decode.field "id" Decode.string)
        (Decode.field "version" Decode.int)
        (Decode.field "favorite" Decode.bool)
        (Decode.field "score" Decode.int)
        (Decode.field "changed" Decode.bool)


reactionRevisionDecoder : Decoder ReactionRevision
reactionRevisionDecoder =
    Decode.map4 ReactionRevision
        (Decode.field "version" Decode.int)
        (Decode.field "favorite" Decode.bool)
        (Decode.field "score" Decode.int)
        (Decode.field "created_at" Decode.string)
