module Domain.Similarity exposing
    ( IndexResponse
    , IndexResult
    , SimilarMatch
    , SimilarPage
    , StatusItem
    , StatusPage
    , algorithm
    , indexResponseDecoder
    , isCanonicalId
    , similarPageDecoder
    , statusPageDecoder
    )

{-| Visual near-copy (dHash) review contracts.

Matches are resized/recompressed near-copies found by perceptual hash
distance, not byte identity and not semantic similarity: crop/flip and
meaning-based matching are out of scope, and even distance 0 can be a
false positive. Byte-identical checks live in the exact-copies UI; never
label a distance-0 match "identical" here. Decoders are strict about
required fields, the canonical algorithm/id forms, and distance bounds,
so a malformed receipt fails as a bad body instead of rendering as an
empty review.
-}

import Domain.Post as Post exposing (PostSummary)
import Json.Decode as Decode exposing (Decoder)


algorithm : String
algorithm =
    "dhash-v1"


type alias SimilarMatch =
    { post : PostSummary
    , distance : Int
    }


type alias SimilarPage =
    { postId : String
    , algorithm : String
    , indexed : Bool
    , matches : List SimilarMatch
    , nextCursor : Maybe String
    }


type alias IndexResult =
    { id : String
    , jobId : String
    , status : String
    }


type alias IndexResponse =
    { posts : List IndexResult }


type alias StatusItem =
    { id : String
    , indexed : Bool
    , jobId : Maybe String
    , status : Maybe String
    }


type alias StatusPage =
    { algorithm : String
    , indexedCount : Int
    , eligibleCount : Int
    , posts : List StatusItem
    , nextCursor : Maybe String
    }


{-| Canonical post id: a positive integer with no sign or leading zeros.
-}
isCanonicalId : String -> Bool
isCanonicalId value =
    case String.toInt value of
        Just parsed ->
            parsed > 0 && String.fromInt parsed == value

        Nothing ->
            False


canonicalId : Decoder String
canonicalId =
    Decode.string
        |> Decode.andThen
            (\value ->
                if isCanonicalId value then
                    Decode.succeed value

                else
                    Decode.fail "id must be a canonical positive integer"
            )


algorithmDecoder : Decoder String
algorithmDecoder =
    Decode.string
        |> Decode.andThen
            (\value ->
                if value == algorithm then
                    Decode.succeed value

                else
                    Decode.fail "algorithm must be dhash-v1"
            )


distanceDecoder : Decoder Int
distanceDecoder =
    Decode.int
        |> Decode.andThen
            (\value ->
                if value >= 0 && value <= 16 then
                    Decode.succeed value

                else
                    Decode.fail "distance must be from 0 to 16"
            )


nonNegativeInt : Decoder Int
nonNegativeInt =
    Decode.int
        |> Decode.andThen
            (\value ->
                if value >= 0 then
                    Decode.succeed value

                else
                    Decode.fail "count must not be negative"
            )


matchDecoder : Decoder SimilarMatch
matchDecoder =
    Decode.map2 SimilarMatch
        (Decode.field "post" Post.decoder)
        (Decode.field "distance" distanceDecoder)


similarPageDecoder : Decoder SimilarPage
similarPageDecoder =
    Decode.map5 SimilarPage
        (Decode.field "post_id" canonicalId)
        (Decode.field "algorithm" algorithmDecoder)
        (Decode.field "indexed" Decode.bool)
        (Decode.field "matches" (Decode.list matchDecoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))


indexResultDecoder : Decoder IndexResult
indexResultDecoder =
    Decode.map3 IndexResult
        (Decode.field "id" canonicalId)
        (Decode.field "job_id" Decode.string)
        (Decode.field "status" Decode.string)


indexResponseDecoder : Decoder IndexResponse
indexResponseDecoder =
    Decode.map IndexResponse
        (Decode.field "posts" (Decode.list indexResultDecoder))


statusItemDecoder : Decoder StatusItem
statusItemDecoder =
    Decode.map4 StatusItem
        (Decode.field "id" canonicalId)
        (Decode.field "indexed" Decode.bool)
        (Decode.field "job_id" (Decode.nullable Decode.string))
        (Decode.field "status" (Decode.nullable Decode.string))


statusPageDecoder : Decoder StatusPage
statusPageDecoder =
    Decode.map5 StatusPage
        (Decode.field "algorithm" algorithmDecoder)
        (Decode.field "indexed_count" nonNegativeInt)
        (Decode.field "eligible_count" nonNegativeInt)
        (Decode.field "posts" (Decode.list statusItemDecoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))
