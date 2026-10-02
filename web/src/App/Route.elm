module App.Route exposing (Route, View(..), fromUrl, toHref)

{-| `?q=…` for the query; `&post=…&view=loupe` while Quick Look is open.
Compare and Survey depend on the selection, which is not part of the URL.
-}

import Url exposing (Url)
import Url.Builder


type View
    = GridView
    | LoupeView


type alias Route =
    { query : String
    , post : Maybe String
    , view : View
    }


fromUrl : Url -> Route
fromUrl url =
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
    , post = post
    , view =
        if param "view" == Just "loupe" && post /= Nothing then
            LoupeView

        else
            GridView
    }


toHref : Route -> String
toHref route =
    let
        query =
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
    Url.Builder.absolute [] (query ++ loupe)


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
