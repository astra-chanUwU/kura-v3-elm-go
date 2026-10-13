module Api.Trash exposing (list, restore, trash)

import Api.Access exposing (authHeaders)
import Domain.Trash exposing (TrashPage, TrashResult, pageDecoder, responseDecoder)
import Http
import Json.Encode as Encode
import Url.Builder


list : String -> Maybe String -> Maybe String -> Int -> (Result Http.Error TrashPage -> msg) -> Cmd msg
list apiBase credential cursor limit toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "posts", "trash" ] ([ Url.Builder.int "limit" limit ] ++ (cursor |> Maybe.map (Url.Builder.string "cursor") |> Maybe.map List.singleton |> Maybe.withDefault []))
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg pageDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


trash : String -> Maybe String -> List String -> (Result Http.Error (List TrashResult) -> msg) -> Cmd msg
trash apiBase credential ids toMsg =
    post apiBase credential [ "api", "posts", "trash" ] ids toMsg


restore : String -> Maybe String -> List String -> (Result Http.Error (List TrashResult) -> msg) -> Cmd msg
restore apiBase credential ids toMsg =
    post apiBase credential [ "api", "posts", "restore" ] ids toMsg


post : String -> Maybe String -> List String -> List String -> (Result Http.Error (List TrashResult) -> msg) -> Cmd msg
post apiBase credential path ids toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase path []
        , body = Http.jsonBody (Encode.object [ ( "post_ids", Encode.list Encode.string ids ) ])
        , expect = Http.expectJson toMsg responseDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


endpoint : String -> List String -> List Url.Builder.QueryParameter -> String
endpoint apiBase path params =
    if String.trim apiBase == "" then
        Url.Builder.absolute path params

    else
        Url.Builder.crossOrigin apiBase path params
