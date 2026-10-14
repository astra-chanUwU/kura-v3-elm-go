module Domain.Post exposing (PostDetail, PostSummary, RatingResponse, RatingResult, RatingRevision, RatingTarget, SearchResponse, TagEditResponse, TagEditResult, TagEditTarget, TagRevision, ReactionResponse, ReactionResult, ReactionTarget, ReactionRevision, MetadataRevision, MetadataTarget, MetadataResult, MetadataResponse, decoder, detailDecoder, metadataResponseDecoder, metadataRevisionDecoder, ratingResponseDecoder, ratingRevisionDecoder, responseDecoder, revisionDecoder, tagEditResponseDecoder, reactionResponseDecoder, summaryOfDetail)

import Domain.Rating as Rating exposing (Rating)
import Json.Decode as Decode exposing (Decoder)


type alias PostSummary =
    { id : String
    , previewUrl : String
    , originalUrl : String
    , mediaType : String
    , width : Int
    , height : Int
    , tags : List String
    , rating : Rating
    , ratingVersion : Maybe Int
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
    , derivativeJobId : Maybe String
    , derivativeStatus : Maybe String
    , metadataVersion : Int
    , metadataHistory : List MetadataRevision
    , rating : Rating
    , ratingVersion : Maybe Int
    , ratingHistory : List RatingRevision
    }


type alias TagRevision =
    { version : Int
    , kind : String
    , addedTags : List String
    , removedTags : List String
    , targetTags : Maybe (List String)
    , revertible : Bool
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


type alias MetadataRevision =
    { version : Int
    , previousSource : String
    , previousArtist : String
    , source : String
    , artist : String
    , createdAt : String
    }


type alias MetadataTarget =
    { id : String
    , version : Int
    }


type alias MetadataResult =
    { id : String
    , version : Int
    , source : String
    , artist : String
    , changed : Bool
    }


type alias MetadataResponse =
    { posts : List MetadataResult }


type alias RatingRevision =
    { version : Int
    , previousRating : Rating
    , rating : Rating
    , createdAt : String
    }


type alias RatingTarget =
    { id : String
    , version : Int
    }


type alias RatingResult =
    { id : String
    , version : Int
    , rating : Rating
    , changed : Bool
    }


type alias RatingResponse =
    { posts : List RatingResult }


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
                , derivativeJobId = Nothing
                , derivativeStatus = Nothing
                , metadataVersion = 0
                , metadataHistory = []
                , rating = Rating.Unrated
                , ratingVersion = Nothing
                , ratingHistory = []
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
                Decode.map8
                    (\tagVersion history favorite score reactionVersion reactionHistory derivativeJobId derivativeStatus ->
                        { detail
                            | tagVersion = tagVersion
                            , history = history
                            , favorite = favorite
                            , score = score
                            , reactionVersion = reactionVersion
                            , reactionHistory = reactionHistory
                            , derivativeJobId = derivativeJobId
                            , derivativeStatus = derivativeStatus
                        }
                    )
                    (Decode.oneOf [ Decode.field "tag_version" Decode.int, Decode.succeed 0 ])
                    (Decode.oneOf [ Decode.field "history" (Decode.list revisionDecoder), Decode.succeed [] ])
                    (Decode.oneOf [ Decode.field "favorite" Decode.bool, Decode.succeed False ])
                    (Decode.oneOf [ Decode.field "score" Decode.int, Decode.succeed 0 ])
                    (Decode.oneOf [ Decode.field "reaction_version" Decode.int, Decode.succeed 0 ])
                    (Decode.oneOf [ Decode.field "reaction_history" (Decode.list reactionRevisionDecoder), Decode.succeed [] ])
                    (Decode.oneOf [ Decode.field "derivative_job_id" Decode.string, Decode.succeed "" ] |> Decode.map emptyToNothing)
                    (Decode.oneOf [ Decode.field "derivative_status" Decode.string, Decode.succeed "" ] |> Decode.map emptyToNothing)
            )
        |> Decode.andThen
            (\detail ->
                Decode.map2
                    (\metadataVersion metadataHistory ->
                        { detail
                            | metadataVersion = metadataVersion
                            , metadataHistory = metadataHistory
                        }
                    )
                    (Decode.oneOf [ Decode.field "metadata_version" Decode.int, Decode.succeed 0 ])
                    (Decode.oneOf [ Decode.field "metadata_history" (Decode.list metadataRevisionDecoder), Decode.succeed [] ])
            )
        |> Decode.andThen
            (\detail ->
                Decode.map3
                    (\rating ratingVersion ratingHistory ->
                        { detail | rating = rating, ratingVersion = ratingVersion, ratingHistory = ratingHistory }
                    )
                    ratingFieldDecoder
                    ratingVersionFieldDecoder
                    (Decode.oneOf [ Decode.field "rating_history" (Decode.list ratingRevisionDecoder), Decode.succeed [] ])
            )


emptyToNothing : String -> Maybe String
emptyToNothing value =
    if String.trim value == "" then
        Nothing

    else
        Just value


{-| Present-but-unknown ratings fail: never assert an unknown value is safe
(or any other rating). A missing rating displays as unrated.
-}
ratingFieldDecoder : Decoder Rating
ratingFieldDecoder =
    Decode.oneOf
        [ Decode.field "rating" (Decode.map Just Decode.value)
        , Decode.succeed Nothing
        ]
        |> Decode.andThen
            (\present ->
                case present of
                    Just _ ->
                        Decode.field "rating" Rating.decoder

                    Nothing ->
                        Decode.succeed Rating.Unrated
            )


{-| A missing version stays unknown (`Nothing`): writes stay disabled until
a proper read supplies one. A present valid zero is `Just 0`; non-integer,
null, or negative values never become a valid zero.
-}
ratingVersionFieldDecoder : Decoder (Maybe Int)
ratingVersionFieldDecoder =
    Decode.oneOf
        [ Decode.field "rating_version" nonNegativeInt |> Decode.map Just
        , Decode.succeed Nothing
        ]


nonNegativeInt : Decoder Int
nonNegativeInt =
    Decode.int
        |> Decode.andThen
            (\value ->
                if value >= 0 then
                    Decode.succeed value

                else
                    Decode.fail "negative version"
            )


decoder : Decoder PostSummary
decoder =
    Decode.map7
        (\id previewUrl originalUrl mediaType width height tags ->
            { id = id
            , previewUrl = previewUrl
            , originalUrl = originalUrl
            , mediaType = mediaType
            , width = width
            , height = height
            , tags = tags
            , rating = Rating.Unrated
            , ratingVersion = Nothing
            }
        )
        (Decode.field "id" Decode.string)
        (Decode.field "preview_url" Decode.string)
        (Decode.field "original_url" Decode.string)
        (Decode.field "media_type" Decode.string)
        (Decode.field "width" Decode.int)
        (Decode.field "height" Decode.int)
        (Decode.oneOf [ Decode.field "tags" (Decode.list Decode.string), Decode.succeed [] ])
        |> Decode.andThen
            (\summary ->
                Decode.map2
                    (\rating ratingVersion -> { summary | rating = rating, ratingVersion = ratingVersion })
                    ratingFieldDecoder
                    ratingVersionFieldDecoder
            )


ratingRevisionDecoder : Decoder RatingRevision
ratingRevisionDecoder =
    Decode.map4 RatingRevision
        (Decode.field "version" Decode.int)
        (Decode.field "previous_rating" Rating.decoder)
        (Decode.field "rating" Rating.decoder)
        (Decode.field "created_at" Decode.string)


ratingResponseDecoder : Decoder RatingResponse
ratingResponseDecoder =
    Decode.map RatingResponse (Decode.field "posts" (Decode.list ratingResultDecoder))


ratingResultDecoder : Decoder RatingResult
ratingResultDecoder =
    Decode.map4 RatingResult
        (Decode.field "id" Decode.string)
        (Decode.field "version" nonNegativeInt)
        (Decode.field "rating" Rating.decoder)
        (Decode.field "changed" Decode.bool)


responseDecoder : Decoder SearchResponse
responseDecoder =
    Decode.map2 SearchResponse
        (Decode.field "posts" (Decode.list decoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))


revisionDecoder : Decoder TagRevision
revisionDecoder =
    Decode.map7
        (\version kind added removed targetTags createdAt revertible ->
            { version = version
            , kind = kind
            , addedTags = added
            , removedTags = removed
            , targetTags = targetTags
            , revertible = revertible
            , createdAt = createdAt
            }
        )
        (Decode.field "version" Decode.int)
        (Decode.field "kind" Decode.string)
        (Decode.field "added_tags" (Decode.list Decode.string))
        (Decode.field "removed_tags" (Decode.list Decode.string))
        (Decode.oneOf [ Decode.field "target_tags" (Decode.nullable (Decode.list Decode.string)), Decode.succeed Nothing ])
        (Decode.field "created_at" Decode.string)
        (Decode.oneOf
            [ Decode.field "revertible" Decode.bool
            , Decode.map (\maybeTags -> maybeTags /= Nothing)
                (Decode.oneOf [ Decode.field "target_tags" (Decode.nullable (Decode.list Decode.string)), Decode.succeed Nothing ])
            ]
        )


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


metadataRevisionDecoder : Decoder MetadataRevision
metadataRevisionDecoder =
    Decode.map6 MetadataRevision
        (Decode.field "version" Decode.int)
        (Decode.oneOf [ Decode.field "previous_source" Decode.string, Decode.succeed "" ])
        (Decode.oneOf [ Decode.field "previous_artist" Decode.string, Decode.succeed "" ])
        (Decode.field "source" Decode.string)
        (Decode.field "artist" Decode.string)
        (Decode.field "created_at" Decode.string)


metadataResponseDecoder : Decoder MetadataResponse
metadataResponseDecoder =
    Decode.map MetadataResponse (Decode.field "posts" (Decode.list metadataResultDecoder))


metadataResultDecoder : Decoder MetadataResult
metadataResultDecoder =
    Decode.map5 MetadataResult
        (Decode.field "id" Decode.string)
        (Decode.field "version" Decode.int)
        (Decode.field "source" Decode.string)
        (Decode.field "artist" Decode.string)
        (Decode.field "changed" Decode.bool)


reactionRevisionDecoder : Decoder ReactionRevision
reactionRevisionDecoder =
    Decode.map4 ReactionRevision
        (Decode.field "version" Decode.int)
        (Decode.field "favorite" Decode.bool)
        (Decode.field "score" Decode.int)
        (Decode.field "created_at" Decode.string)


{-| The grid-cache projection of a detail record. Upload confirmations and
thumbnail refreshes use it to update cached previews in place.
-}
summaryOfDetail : PostDetail -> PostSummary
summaryOfDetail detail =
    { id = detail.id
    , previewUrl = detail.previewUrl
    , originalUrl = detail.originalUrl
    , mediaType = detail.mediaType
    , width = detail.width
    , height = detail.height
    , tags = detail.tags
    , rating = detail.rating
    , ratingVersion = detail.ratingVersion
    }
