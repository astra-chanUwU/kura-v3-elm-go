module Domain.Duplicates exposing
    ( DuplicateGroup
    , DuplicateGroupsPage
    , DuplicateMembersPage
    , groupsPageDecoder
    , isCanonicalHash
    , membersPageDecoder
    )

{-| Exact-copy (identical original bytes) review contracts.

A group is one canonical `sha256:` hash shared by at least two visible
posts. This is byte identity only: it says nothing about visual
similarity. Decoders are strict about required fields, the canonical
hash form, and a member count of at least two, so a malformed receipt
fails as a bad body instead of rendering as an empty review.
-}

import Domain.Post as Post exposing (PostSummary)
import Json.Decode as Decode exposing (Decoder)


type alias DuplicateGroup =
    { hash : String
    , count : Int
    , posts : List PostSummary
    }


type alias DuplicateGroupsPage =
    { groups : List DuplicateGroup
    , nextCursor : Maybe String
    }


type alias DuplicateMembersPage =
    { hash : String
    , count : Int
    , posts : List PostSummary
    , nextCursor : Maybe String
    }


{-| Canonical content hash: `sha256:` followed by 64 lowercase hex chars.
-}
isCanonicalHash : String -> Bool
isCanonicalHash value =
    String.startsWith "sha256:" value
        && String.length value == 71
        && String.all isLowerHex (String.dropLeft 7 value)


isLowerHex : Char -> Bool
isLowerHex char =
    Char.isDigit char || (char >= 'a' && char <= 'f')


canonicalHash : Decoder String
canonicalHash =
    Decode.string
        |> Decode.andThen
            (\value ->
                if isCanonicalHash value then
                    Decode.succeed value

                else
                    Decode.fail "hash must be sha256 followed by 64 lowercase hexadecimal characters"
            )


groupCount : Decoder Int
groupCount =
    Decode.int
        |> Decode.andThen
            (\value ->
                if value >= 2 then
                    Decode.succeed value

                else
                    Decode.fail "duplicate group count must be at least 2"
            )


groupDecoder : Decoder DuplicateGroup
groupDecoder =
    Decode.map3 DuplicateGroup
        (Decode.field "hash" canonicalHash)
        (Decode.field "count" groupCount)
        (Decode.field "posts" (Decode.list Post.decoder))


groupsPageDecoder : Decoder DuplicateGroupsPage
groupsPageDecoder =
    Decode.map2 DuplicateGroupsPage
        (Decode.field "groups" (Decode.list groupDecoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))


membersPageDecoder : Decoder DuplicateMembersPage
membersPageDecoder =
    Decode.map4 DuplicateMembersPage
        (Decode.field "hash" canonicalHash)
        (Decode.field "count" groupCount)
        (Decode.field "posts" (Decode.list Post.decoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))
