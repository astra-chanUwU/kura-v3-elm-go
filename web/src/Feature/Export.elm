module Feature.Export exposing (collectionUrl, selectionUrl, validCollection, validSelection)

import Url.Builder


{-| Public export URLs. Never carry a token in URL or header; the
browser downloads via a native link so the archive is not buffered
in Elm. Metadata defaults to true at call sites.
-}
selectionUrl : String -> List String -> Bool -> Maybe String
selectionUrl apiBase ids metadata =
    if validSelection ids then
        Just (endpoint apiBase [ "api", "posts", "export" ] [ Url.Builder.string "ids" (String.join "," ids), Url.Builder.string "metadata" (bool metadata) ])

    else
        Nothing


collectionUrl : String -> String -> Maybe Int -> Bool -> Maybe String
collectionUrl apiBase collectionId version metadata =
    case version of
        Just v ->
            if v >= 0 && String.trim collectionId /= "" then
                Just (endpoint apiBase [ "api", "collections", collectionId, "export" ] [ Url.Builder.int "expected_version" v, Url.Builder.string "metadata" (bool metadata) ])

            else
                Nothing

        Nothing ->
            Nothing


validSelection : List String -> Bool
validSelection ids =
    not (List.isEmpty ids) && List.length ids <= 1000


validCollection : Maybe Int -> Bool
validCollection version =
    case version of
        Just v ->
            v >= 0

        Nothing ->
            False


bool : Bool -> String
bool b =
    if b then
        "true"

    else
        "false"


endpoint : String -> List String -> List Url.Builder.QueryParameter -> String
endpoint apiBase path params =
    if String.trim apiBase == "" then
        Url.Builder.absolute path params

    else
        Url.Builder.crossOrigin apiBase path params
