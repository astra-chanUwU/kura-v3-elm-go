module Domain.TagManagement exposing
    ( ListPage
    , Mode(..)
    , Preview
    , Receipt
    , TagRow
    , listPageDecoder
    , maxSourcePosts
    , modeFromString
    , modeToString
    , previewDecoder
    , receiptDecoder
    , receiptMatchesPreview
    , safeCountDecoder
    , safeVersionDecoder
    , validateTag
    )

{-| Contracts for global tag rename/merge. Decoders are strict: missing,
null, empty, negative, non-integer, or JS-unsafe numbers fail decoding.
-}

import Char
import Json.Decode as Decode exposing (Decoder)


maxSourcePosts : Int
maxSourcePosts =
    1000


maxSafeInteger : Float
maxSafeInteger =
    9007199254740991


type Mode
    = Rename
    | Merge


modeToString : Mode -> String
modeToString mode =
    case mode of
        Rename ->
            "rename"

        Merge ->
            "merge"


modeFromString : String -> Maybe Mode
modeFromString value =
    case value of
        "rename" ->
            Just Rename

        "merge" ->
            Just Merge

        _ ->
            Nothing


type alias TagRow =
    { tag : String
    , postCount : Int
    , visiblePostCount : Int
    }


type alias ListPage =
    { version : Int
    , tags : List TagRow
    , nextCursor : Maybe String
    }


type alias Preview =
    { version : Int
    , mode : Mode
    , sourceTag : String
    , targetTag : String
    , affectedPostCount : Int
    , visiblePostCount : Int
    , targetPostCount : Int
    }


type alias Receipt =
    { id : String
    , version : Int
    , mode : Mode
    , sourceTag : String
    , targetTag : String
    , affectedPostCount : Int
    , visiblePostCount : Int
    , changed : Bool
    }


{-| Trim and validate a tag: 1-128 Unicode code points, no NUL.
Code points are counted with String.toList, not String.length: the
latter counts UTF-16 code units and would reject emoji tags that the
server accepts.
-}
validateTag : String -> Result String String
validateTag raw =
    let
        trimmed =
            String.trim raw
    in
    if trimmed == "" then
        Err "Enter a tag."

    else if (String.toList trimmed |> List.length) > 128 then
        Err "Tags are at most 128 characters."

    else if String.contains "\u{0000}" trimmed then
        Err "Tags cannot contain NUL."

    else
        Ok trimmed


safeVersionDecoder : Decoder Int
safeVersionDecoder =
    safeCountDecoder


safeCountDecoder : Decoder Int
safeCountDecoder =
    Decode.int |> Decode.andThen nonNegativeSafe


nonNegativeSafe : Int -> Decoder Int
nonNegativeSafe value =
    if value < 0 || toFloat value > maxSafeInteger then
        Decode.fail "expected a non-negative safe integer"

    else
        Decode.succeed value


{-| A receipt id is a canonical positive decimal: digits only, no
leading zero, and not zero itself (for example "1", never "op-1").
-}
isCanonicalReceiptId : String -> Bool
isCanonicalReceiptId value =
    case String.toList value of
        [] ->
            False

        '0' :: _ ->
            False

        chars ->
            List.all Char.isDigit chars


receiptIdDecoder : Decoder String
receiptIdDecoder =
    Decode.string
        |> Decode.andThen
            (\value ->
                if isCanonicalReceiptId value then
                    Decode.succeed value

                else
                    Decode.fail "expected a canonical positive decimal receipt id"
            )


tagString : Decoder String
tagString =
    Decode.string
        |> Decode.andThen
            (\value ->
                case validateTag value of
                    Ok tag ->
                        if tag == value then
                            Decode.succeed value

                        else
                            Decode.fail "tag is not trimmed"

                    Err message ->
                        Decode.fail message
            )


modeDecoder : Decoder Mode
modeDecoder =
    Decode.string
        |> Decode.andThen
            (\value ->
                case modeFromString value of
                    Just mode ->
                        Decode.succeed mode

                    Nothing ->
                        Decode.fail "unknown mode"
            )


tagRowDecoder : Decoder TagRow
tagRowDecoder =
    Decode.map3 TagRow
        (Decode.field "tag" tagString)
        (Decode.field "post_count" safeCountDecoder)
        (Decode.field "visible_post_count" safeCountDecoder)
        |> Decode.andThen
            (\row ->
                if row.postCount < 1 || row.visiblePostCount > row.postCount then
                    Decode.fail "tag row counts are inconsistent"

                else
                    Decode.succeed row
            )


listPageDecoder : Decoder ListPage
listPageDecoder =
    Decode.map3 ListPage
        (Decode.field "version" safeVersionDecoder)
        (Decode.field "tags" (Decode.list tagRowDecoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))
        |> Decode.andThen
            (\page ->
                case page.nextCursor of
                    Just cursor ->
                        if cursor == "" then
                            Decode.fail "empty cursor"

                        else
                            Decode.succeed page

                    Nothing ->
                        Decode.succeed page
            )


previewDecoder : Decoder Preview
previewDecoder =
    Decode.map7 Preview
        (Decode.field "version" safeVersionDecoder)
        (Decode.field "mode" modeDecoder)
        (Decode.field "source_tag" tagString)
        (Decode.field "target_tag" tagString)
        (Decode.field "affected_post_count" safeCountDecoder)
        (Decode.field "visible_post_count" safeCountDecoder)
        (Decode.field "target_post_count" safeCountDecoder)
        |> Decode.andThen
            (\preview ->
                if
                    preview.affectedPostCount
                        < 1
                        || preview.affectedPostCount
                        > maxSourcePosts
                        || preview.visiblePostCount
                        > preview.affectedPostCount
                        || preview.sourceTag
                        == preview.targetTag
                        || (preview.mode == Rename && preview.targetPostCount /= 0)
                        || (preview.mode == Merge && preview.targetPostCount < 1)
                then
                    Decode.fail "preview counts or tags are inconsistent"

                else
                    Decode.succeed preview
            )


receiptDecoder : Decoder Receipt
receiptDecoder =
    Decode.map8 Receipt
        (Decode.field "id" receiptIdDecoder)
        (Decode.field "version" safeVersionDecoder)
        (Decode.field "mode" modeDecoder)
        (Decode.field "source_tag" tagString)
        (Decode.field "target_tag" tagString)
        (Decode.field "affected_post_count" safeCountDecoder)
        (Decode.field "visible_post_count" safeCountDecoder)
        (Decode.field "changed" Decode.bool)
        |> Decode.andThen
            (\receipt ->
                if
                    not receipt.changed
                        || receipt.affectedPostCount
                        < 1
                        || receipt.affectedPostCount
                        > maxSourcePosts
                        || receipt.visiblePostCount
                        > receipt.affectedPostCount
                then
                    Decode.fail "receipt counts are inconsistent"

                else
                    Decode.succeed receipt
            )


{-| A receipt is valid only for the exact preview it confirms. The
visible count may drift because visibility can change outside the
global tag version, so it is not compared to the preview, but it must
still lie within 0..affected. The affected count (1..1000), mode,
tags, and `changed` must match, the id must be a canonical positive
decimal, and `version` must be `preview.version + 1` computed without
leaving the safe-integer range.
-}
receiptMatchesPreview : Preview -> Receipt -> Bool
receiptMatchesPreview preview receipt =
    receipt.mode
        == preview.mode
        && receipt.sourceTag
        == preview.sourceTag
        && receipt.targetTag
        == preview.targetTag
        && toFloat preview.version
        < maxSafeInteger
        && receipt.version
        == preview.version
        + 1
        && receipt.changed
        && receipt.affectedPostCount
        >= 1
        && receipt.affectedPostCount
        <= maxSourcePosts
        && receipt.affectedPostCount
        == preview.affectedPostCount
        && receipt.visiblePostCount
        >= 0
        && receipt.visiblePostCount
        <= receipt.affectedPostCount
        && isCanonicalReceiptId receipt.id
