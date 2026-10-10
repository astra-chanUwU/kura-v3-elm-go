module Api.Collection exposing (addPosts, create, list, posts, removePost, reorder)

{-| Collection reads (`list`, `posts`) are public and never credentialed.
Mutations (`create`, `addPosts`, `removePost`, `reorder`) take the unlocked
credential and send it as an `Authorization` header.
-}

import Api.Access exposing (authHeaders)
import Domain.Collection exposing (Collection, PostsResponse, decoder, responseDecoder)
import Domain.Post
import Http
import Json.Encode as Encode
import String
import Url.Builder


list : String -> (Result Http.Error (List Collection) -> msg) -> Cmd msg
list apiBase toMsg =
    Http.get { url = endpoint apiBase [ "api", "collections" ], expect = Http.expectJson toMsg responseDecoder }


posts : String -> String -> Int -> (Result Http.Error PostsResponse -> msg) -> Cmd msg
posts apiBase collectionId limit toMsg =
    Http.get
        { url = endpointWithLimit apiBase [ "api", "collections", collectionId, "posts" ] limit
        , expect = Http.expectJson toMsg Domain.Post.responseDecoder
        }


create : String -> Maybe String -> String -> (Result Http.Error Collection -> msg) -> Cmd msg
create apiBase credential name toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections" ]
        , body = Http.jsonBody (Encode.object [ ( "name", Encode.string name ) ])
        , expect = Http.expectJson toMsg decoder
        , timeout = Nothing
        , tracker = Nothing
        }


addPosts : String -> Maybe String -> String -> List String -> (Result Http.Error Collection -> msg) -> Cmd msg
addPosts apiBase credential collectionId postIds toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections", collectionId, "posts" ]
        , body = Http.jsonBody (Encode.object [ ( "post_ids", Encode.list Encode.string postIds ) ])
        , expect = Http.expectJson toMsg decoder
        , timeout = Nothing
        , tracker = Nothing
        }


removePost : String -> Maybe String -> String -> String -> (Result Http.Error () -> msg) -> Cmd msg
removePost apiBase credential collectionId postId toMsg =
    Http.request
        { method = "DELETE"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections", collectionId, "posts", postId ]
        , body = Http.emptyBody
        , expect = Http.expectWhatever toMsg
        , timeout = Nothing
        , tracker = Nothing
        }


reorder : String -> Maybe String -> String -> List String -> (Result Http.Error Collection -> msg) -> Cmd msg
reorder apiBase credential collectionId postIds toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections", collectionId, "order" ]
        , body = Http.jsonBody (Encode.object [ ( "post_ids", Encode.list Encode.string postIds ) ])
        , expect = Http.expectJson toMsg decoder
        , timeout = Nothing
        , tracker = Nothing
        }


endpoint : String -> List String -> String
endpoint apiBase path =
    if String.trim apiBase == "" then
        Url.Builder.absolute path []

    else
        Url.Builder.crossOrigin apiBase path []


endpointWithLimit : String -> List String -> Int -> String
endpointWithLimit apiBase path limit =
    if String.trim apiBase == "" then
        Url.Builder.absolute path [ Url.Builder.int "limit" limit ]

    else
        Url.Builder.crossOrigin apiBase path [ Url.Builder.int "limit" limit ]
