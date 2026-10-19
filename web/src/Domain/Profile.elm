module Domain.Profile exposing
    ( Draft
    , Profile
    , applyRemoteVersion
    , decoder
    , draftFrom
    , initials
    , isDirty
    , normalizeUsername
    , safeCover
    , validWebsite
    )

import Json.Decode as Decode exposing (Decoder)


type alias Profile =
    { id : Int
    , username : String
    , displayName : String
    , bio : String
    , website : String
    , avatarUrl : String
    , version : Int
    , canEdit : Bool
    }


type alias Draft =
    { displayName : String
    , username : String
    , bio : String
    , website : String
    , expectedVersion : Int
    }


decoder : Decoder Profile
decoder =
    Decode.map8 Profile
        (Decode.field "id" Decode.int)
        (Decode.field "username" Decode.string)
        (Decode.field "display_name" Decode.string)
        (Decode.field "bio" Decode.string)
        (Decode.field "website" Decode.string)
        (Decode.field "avatar_url" Decode.string)
        (Decode.field "version" Decode.int)
        (Decode.field "can_edit" Decode.bool)


draftFrom : Profile -> Draft
draftFrom profile =
    { displayName = profile.displayName
    , username = profile.username
    , bio = profile.bio
    , website = profile.website
    , expectedVersion = profile.version
    }


{-| A stale save keeps the typed draft and adopts the server version. -}
applyRemoteVersion : Profile -> Draft -> Draft
applyRemoteVersion profile draft =
    { draft | expectedVersion = profile.version }


isDirty : Profile -> Draft -> Bool
isDirty profile draft =
    String.trim draft.displayName /= profile.displayName
        || normalizeUsername draft.username /= Ok profile.username
        || String.trim draft.bio /= profile.bio
        || String.trim draft.website /= profile.website


initials : String -> String
initials raw =
    let
        parts =
            raw
                |> String.replace "_" " "
                |> String.replace "-" " "
                |> String.words
                |> List.filter (not << String.isEmpty)
                |> List.take 2
                |> List.filterMap (String.uncons >> Maybe.map (Tuple.first >> String.fromChar >> String.toUpper))
    in
    case parts of
        [] ->
            "?"

        _ ->
            String.concat parts


normalizeUsername : String -> Result String String
normalizeUsername raw =
    let
        name =
            String.toLower (String.trim raw)
    in
    if String.isEmpty name || String.length name > 32 then
        Err "username must be 1-32 characters of letters, digits, '_' or '-'"

    else if not (validChars name) then
        Err "username must be 1-32 characters of letters, digits, '_' or '-'"

    else if reserved name then
        Err "username is reserved"

    else
        Ok name


validWebsite : String -> Result String String
validWebsite raw =
    let
        value =
            String.trim raw
    in
    if value == "" then
        Ok ""

    else if String.length value > 200 || String.contains " " value || String.contains "\\" value || String.contains "@" value then
        Err "website must be empty or an http(s) URL"

    else if not (String.startsWith "https://" value || String.startsWith "http://" value) then
        Err "website must be empty or an http(s) URL"

    else
        Ok value


safeCover : String -> Maybe String
safeCover raw =
    if String.startsWith "/media/" raw && not (String.contains ".." raw) && not (String.contains "\\" raw) && not (String.contains "//" raw) then
        Just raw

    else
        Nothing


validChars : String -> Bool
validChars name =
    case String.uncons name of
        Nothing ->
            False

        Just ( first, rest ) ->
            isAlnum first && String.all (\char -> isAlnum char || char == '_' || char == '-') rest


isAlnum : Char -> Bool
isAlnum char =
    (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')


reserved : String -> Bool
reserved name =
    List.member name
        [ "local"
        , "system"
        , "sign-in"
        , "sign-up"
        , "trouble"
        , "api"
        , "media"
        , "health"
        , "static"
        , "dist"
        , "u"
        , "profile"
        , "profiles"
        , "index"
        , "assets"
        , "icons"
        , "brand"
        , "favicon"
        ]
