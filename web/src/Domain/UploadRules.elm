module Domain.UploadRules exposing (maxBytes)

{-| Client-side upload limit shared by API request validation and the
pure upload queue.
-}


maxBytes : Int
maxBytes =
    25 * 1024 * 1024
