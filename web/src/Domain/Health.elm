module Domain.Health exposing (BackupRef, KnownBytes, KnownCount, Page, Repair, Scan, Summary, pageDecoder, repairDecoder, scanDecoder, summaryDecoder)

import Json.Decode as Decode exposing (Decoder)


type alias KnownCount =
    { known : Bool
    , value : Int
    }


type alias KnownBytes =
    { known : Bool
    , value : Float
    }


type alias BackupRef =
    { id : String
    , createdAt : String
    }


type alias Summary =
    { missingOriginals : KnownCount
    , missingThumbnails : KnownCount
    , failedThumbnails : KnownCount
    , processingBacklog : KnownCount
    , storageBytes : KnownBytes
    , latestVerifiedBackup : Maybe BackupRef
    , media : String
    , s3Checks : String
    }


type alias Scan =
    { status : String
    , checked : Int
    , missing : Int
    , corrupt : Int
    , bytes : Float
    , truncated : Bool
    , error : String
    }


type alias Page =
    { postIds : List String
    , truncated : Bool
    }


type alias Repair =
    { jobId : String
    , status : String
    }


summaryDecoder : Decoder Summary
summaryDecoder =
    Decode.map8 Summary
        (Decode.field "missing_originals" knownCountDecoder)
        (Decode.field "missing_thumbnails" knownCountDecoder)
        (Decode.field "failed_thumbnails" knownCountDecoder)
        (Decode.field "processing_backlog" knownCountDecoder)
        (Decode.field "storage_bytes" knownBytesDecoder)
        (Decode.field "latest_verified_backup" (Decode.nullable backupDecoder))
        (Decode.field "media" Decode.string)
        (Decode.oneOf [ Decode.field "s3_checks" Decode.string, Decode.succeed "" ])


scanDecoder : Decoder Scan
scanDecoder =
    Decode.map7 Scan
        (Decode.field "status" Decode.string)
        (Decode.field "checked" Decode.int)
        (Decode.field "missing" Decode.int)
        (Decode.field "corrupt" Decode.int)
        (Decode.field "bytes" Decode.float)
        (Decode.field "truncated" Decode.bool)
        (Decode.oneOf [ Decode.field "error" Decode.string, Decode.succeed "" ])


pageDecoder : Decoder Page
pageDecoder =
    Decode.map2 Page
        (Decode.field "post_ids" (Decode.list Decode.string))
        (Decode.field "truncated" Decode.bool)


repairDecoder : Decoder Repair
repairDecoder =
    Decode.map2 Repair
        (Decode.field "job_id" Decode.string)
        (Decode.field "status" Decode.string)


knownCountDecoder : Decoder KnownCount
knownCountDecoder =
    Decode.map2 KnownCount
        (Decode.field "known" Decode.bool)
        (Decode.field "value" Decode.int)


knownBytesDecoder : Decoder KnownBytes
knownBytesDecoder =
    Decode.map2 KnownBytes
        (Decode.field "known" Decode.bool)
        (Decode.field "value" Decode.float)


backupDecoder : Decoder BackupRef
backupDecoder =
    Decode.map2 BackupRef
        (Decode.field "id" Decode.string)
        (Decode.field "created_at" Decode.string)
