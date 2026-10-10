module Api.Collection exposing (addPosts, create, list, posts, removePost, reorder)

{-| Collection reads (`list`, `posts`) are public and never credentialed.
Mutations (`create`, `addPosts`, `removePost`, `reorder`) take the unlocked
credential and send it as an `Authorization` header.
-}

import Api.Access exposing (authHeaders)
import Domain.Collection exposing (Collection, CollectionPostsPage, decoder, postsDecoder, responseDecoder)
import Http
import Json.Encode as Encode
import String
import Url.Builder


list : String -> (Result Http.Error (List Collection) -> msg) -> Cmd msg
list apiBase toMsg =
    Http.get { url = endpoint apiBase [ "api", "collections" ], expect = Http.expectJson toMsg responseDecoder }


{-| Fetch one versioned page of collection members. A `Nothing` cursor
starts from the beginning; a `Just` cursor continues after its key at
the exact collection version it was issued for, so a concurrent
membership/order change answers 409 instead of silently skipping rows.
-}
posts : String -> String -> Maybe String -> Int -> (Result Http.Error CollectionPostsPage -> msg) -> Cmd msg
posts apiBase collectionId cursor limit toMsg =
    Http.get
        { url = endpointWithCursor apiBase [ "api", "collections", collectionId, "posts" ] cursor limit
        , expect = Http.expectJson toMsg postsDecoder
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


endpointWithCursor : String -> List String -> Maybe String -> Int -> String
endpointWithCursor apiBase path cursor limit =
    let
        params =
            [ Url.Builder.int "limit" limit ]
                ++ (case cursor of
                        Just value ->
                            [ Url.Builder.string "cursor" value ]

                        Nothing ->
                            []
                   )
    in
    if String.trim apiBase == "" then
        Url.Builder.absolute path params

    else
        Url.Builder.crossOrigin apiBase path params
