module App.Route exposing
    ( CollectionRef(..)
    , ProfileRef(..)
    , Route
    , View(..)
    , Workspace(..)
    , forCollection
    , forProfile
    , forSearch
    , fromUrl
    , profileOf
    , toHref
    , workspace
    )

{-| Library location.

`?q=…` is a search. `?collection=…` is one collection, identified by the
same canonical positive id the API accepts. A valid collection id wins
when both are present, and only that winning source is written back, so
the address bar matches the Navigator. `&post=…&view=loupe` opens Quick
Look on that source. Compare, Survey, selection, and page cursors stay
out of the URL.
-}

import Url exposing (Url)
import Url.Builder


type View
    = GridView
    | LoupeView


{-| Collection identity carried by a route. `RejectedCollection` means
the parameter was present but not a canonical id; it does not fall
through to search.
-}
type CollectionRef
    = NoCollection
    | Collection String
    | RejectedCollection


type alias Route =
    { query : String
    , collection : CollectionRef
    , post : Maybe String
    , view : View
    , profile : ProfileRef
    }


{-| Public profile identity. `RejectedProfile` is a `/u/…` path that is
not a username, so it does not fall through to the library.
-}
type ProfileRef
    = NoProfile
    | Profile String
    | RejectedProfile


{-| Which result set the route identifies. Collection wins over `q`.
-}
type Workspace
    = SearchWorkspace String
    | CollectionWorkspace String
    | InvalidCollection


workspace : Route -> Workspace
workspace route =
    case route.collection of
        Collection id ->
            CollectionWorkspace id

        RejectedCollection ->
            InvalidCollection

        NoCollection ->
            SearchWorkspace route.query


{-| Search route that keeps the current Quick Look target and drops any
collection. Submitting a search leaves the collection workspace.
-}
forSearch : Route -> String -> Route
forSearch route query =
    { route | query = query, collection = NoCollection, profile = NoProfile }


{-| Grid route for one validated collection. `Nothing` when the id is
not canonical, so a bad id is never written into the URL.
-}
forCollection : String -> Maybe Route
forCollection raw =
    case validCollectionId raw of
        Just id ->
            Just
                { query = ""
                , collection = Collection id
                , post = Nothing
                , view = GridView
                , profile = NoProfile
                }

        Nothing ->
            Nothing


{-| Grid route for one public profile. The username is stored lowercase. -}
forProfile : String -> Route
forProfile name =
    { query = ""
    , collection = NoCollection
    , post = Nothing
    , view = GridView
    , profile = Profile (String.toLower name)
    }


profileOf : Route -> ProfileRef
profileOf route =
    route.profile


fromUrl : Url -> Route
fromUrl url =
    case profileFromPath url.path of
        Just ref ->
            { query = ""
            , collection = NoCollection
            , post = Nothing
            , view = GridView
            , profile = ref
            }

        Nothing ->
            let
                params =
                    url.query |> Maybe.map parseParams |> Maybe.withDefault []

                param name =
                    params
                        |> List.filter (\( key, _ ) -> key == name)
                        |> List.head
                        |> Maybe.map Tuple.second

                post =
                    param "post" |> Maybe.map String.trim |> Maybe.andThen nonEmpty
            in
            { query = param "q" |> Maybe.withDefault ""
            , collection = collectionFrom param
            , post = post
            , view =
                if param "view" == Just "loupe" && post /= Nothing then
                    LoupeView

                else
                    GridView
            , profile = NoProfile
            }


toHref : Route -> String
toHref route =
    case route.profile of
        Profile name ->
            Url.Builder.absolute [ "u", name ] []

        RejectedProfile ->
            "/u"

        NoProfile ->
            let
                source =
                    case route.collection of
                        Collection id ->
                            [ Url.Builder.string "collection" id ]

                        RejectedCollection ->
                            []

                        NoCollection ->
                            if String.trim route.query == "" then
                                []

                            else
                                [ Url.Builder.string "q" route.query ]

                loupe =
                    case ( route.view, route.post ) of
                        ( LoupeView, Just post ) ->
                            [ Url.Builder.string "post" post, Url.Builder.string "view" "loupe" ]

                        _ ->
                            []
            in
            Url.Builder.absolute [] (source ++ loupe)


profileFromPath : String -> Maybe ProfileRef
profileFromPath path =
    if path == "/u" || path == "/u/" then
        Just RejectedProfile

    else
        case String.split "/" path of
            "" :: "u" :: segment :: rest ->
                if rest /= [] || String.isEmpty segment then
                    Just RejectedProfile

                else if validProfileSegment segment then
                    Just (Profile (String.toLower segment))

                else
                    Just RejectedProfile

            _ ->
                Nothing


validProfileSegment : String -> Bool
validProfileSegment raw =
    if String.isEmpty raw || String.length raw > 32 || String.contains "." raw then
        False

    else
        case String.uncons raw of
            Nothing ->
                False

            Just ( first, rest ) ->
                isProfileAlnum first
                    && String.all (\char -> isProfileAlnum char || char == '_' || char == '-') rest


isProfileAlnum : Char -> Bool
isProfileAlnum char =
    (char >= 'a' && char <= 'z')
        || (char >= 'A' && char <= 'Z')
        || (char >= '0' && char <= '9')


collectionFrom : (String -> Maybe String) -> CollectionRef
collectionFrom param =
    case param "collection" |> Maybe.map String.trim |> Maybe.andThen nonEmpty of
        Nothing ->
            NoCollection

        Just raw ->
            case validCollectionId raw of
                Just id ->
                    Collection id

                Nothing ->
                    RejectedCollection


{-| Canonical collection id: ASCII digits, no leading zero, and within
the signed 64-bit range the server parses.
-}
validCollectionId : String -> Maybe String
validCollectionId raw =
    let
        value =
            String.trim raw
    in
    if isCanonicalId value then
        Just value

    else
        Nothing


isCanonicalId : String -> Bool
isCanonicalId value =
    String.all isAsciiDigit value
        && String.length value >= 1
        && String.length value <= 19
        && not (String.startsWith "0" value)
        && (String.length value < 19 || value <= "9223372036854775807")


isAsciiDigit : Char -> Bool
isAsciiDigit char =
    char >= '0' && char <= '9'


parseParams : String -> List ( String, String )
parseParams query =
    query
        |> String.split "&"
        |> List.filterMap
            (\part ->
                case String.split "=" part of
                    name :: valueParts ->
                        Just ( name, decode (String.join "=" valueParts) )

                    [] ->
                        Nothing
            )


decode : String -> String
decode value =
    value
        |> String.replace "+" " "
        |> Url.percentDecode
        |> Maybe.withDefault ""


nonEmpty : String -> Maybe String
nonEmpty value =
    if value == "" then
        Nothing

    else
        Just value
