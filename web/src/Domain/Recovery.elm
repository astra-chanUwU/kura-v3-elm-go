module Domain.Recovery exposing (Backup, Listing, Receipt, backupDecoder, listingDecoder, receiptDecoder)

import Json.Decode as Decode exposing (Decoder)


type alias Backup =
    { id : String
    , createdAt : String
    , mediaCount : Int
    , bytes : Float
    , status : String
    }


type alias Listing =
    { configured : Bool
    , backups : List Backup
    , truncated : Bool
    }


type alias Receipt =
    { id : String
    , verifiedAt : String
    , mediaCount : Int
    , bytes : Float
    }


backupDecoder : Decoder Backup
backupDecoder =
    Decode.map5 Backup
        (Decode.field "id" Decode.string)
        (Decode.field "created_at" Decode.string)
        (Decode.field "media_count" Decode.int)
        (Decode.field "bytes" Decode.float)
        (Decode.field "status" (Decode.string |> Decode.andThen validStatus))


validStatus : String -> Decoder String
validStatus value =
    if value == "available" || value == "invalid" then
        Decode.succeed value

    else
        Decode.fail "unknown backup status"


listingDecoder : Decoder Listing
listingDecoder =
    Decode.map3 Listing
        (Decode.field "configured" Decode.bool)
        (Decode.field "backups" (Decode.list backupDecoder |> Decode.andThen bounded))
        (Decode.field "truncated" Decode.bool)


bounded : List Backup -> Decoder (List Backup)
bounded backups =
    if List.length backups <= 100 then
        Decode.succeed backups

    else
        Decode.fail "backup listing exceeds its bound"


receiptDecoder : Decoder Receipt
receiptDecoder =
    Decode.map4 Receipt
        (Decode.field "id" Decode.string)
        (Decode.field "verified_at" Decode.string)
        (Decode.field "media_count" Decode.int)
        (Decode.field "bytes" Decode.float)
