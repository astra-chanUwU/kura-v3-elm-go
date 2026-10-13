module Api.Collection exposing (addPosts, create, deleteCollection, list, move, posts, removePost, rename, reorder, summaries)

{-| Collection reads (`summaries`, `posts`, and the legacy `list`) are
public and never credentialed. Mutations take the unlocked credential and
send it as an `Authorization` header.

The library UI initializes and refreshes navigation from `summaries`
only; `list` and `reorder` remain for external compatibility and are
never called by the UI, because both traffic in whole member arrays.
-}

import Api.Access exposing (authHeaders)
import Domain.Collection exposing (Collection, CollectionAck, CollectionPostsPage, CollectionSummary, ManageResult, MoveResult, ackDecoder, decoder, manageDecoder, moveDecoder, postsDecoder, responseDecoder, summariesDecoder)
import Http
import Json.Encode as Encode
import String
import Url.Builder


{-| Legacy whole-member list. External compatibility only.
-}
list : String -> (Result Http.Error (List Collection) -> msg) -> Cmd msg
list apiBase toMsg =
    Http.get { url = endpoint apiBase [ "api", "collections" ], expect = Http.expectJson toMsg responseDecoder }


{-| Lightweight collection summaries for navigation: counts and versions
only, never member IDs, however large a collection grows.
-}
summaries : String -> (Result Http.Error (List CollectionSummary) -> msg) -> Cmd msg
summaries apiBase toMsg =
    Http.get { url = endpoint apiBase [ "api", "collections", "summaries" ], expect = Http.expectJson toMsg summariesDecoder }


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


create : String -> Maybe String -> String -> (Result Http.Error CollectionAck -> msg) -> Cmd msg
create apiBase credential name toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections" ]
        , body = Http.jsonBody (Encode.object [ ( "name", Encode.string name ) ])
        , expect = Http.expectJson toMsg ackDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


addPosts : String -> Maybe String -> String -> List String -> (Result Http.Error CollectionAck -> msg) -> Cmd msg
addPosts apiBase credential collectionId postIds toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections", collectionId, "posts" ]
        , body = Http.jsonBody (Encode.object [ ( "post_ids", Encode.list Encode.string postIds ) ])
        , expect = Http.expectJson toMsg ackDecoder
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


{-| Legacy whole-array reorder. External compatibility only; the UI
moves single posts through `move` so hidden members stay server-owned.
-}
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


{-| Move one member before `beforePostId`, or to the end when it is
`Nothing`, guarded by the observed collection version. A missing version
cannot be expressed here: callers must pass the real observed version
(zero included) and never invent one.
-}
move : String -> Maybe String -> String -> String -> Maybe String -> Int -> (Result Http.Error MoveResult -> msg) -> Cmd msg
move apiBase credential collectionId postId beforePostId expectedVersion toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections", collectionId, "move" ]
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "post_id", Encode.string postId )
                    , ( "before_post_id", Maybe.map Encode.string beforePostId |> Maybe.withDefault Encode.null )
                    , ( "expected_version", Encode.int expectedVersion )
                    ]
                )
        , expect = Http.expectJson toMsg moveDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


{-| Rename a collection, guarded by its observed version.
-}
rename : String -> Maybe String -> String -> String -> Int -> (Result Http.Error ManageResult -> msg) -> Cmd msg
rename apiBase credential collectionId name expectedVersion toMsg =
    Http.request
        { method = "PATCH"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections", collectionId ]
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "name", Encode.string name )
                    , ( "expected_version", Encode.int expectedVersion )
                    ]
                )
        , expect = Http.expectJson toMsg manageDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


{-| Delete a collection and its memberships only (never its posts),
guarded by its observed version.
-}
deleteCollection : String -> Maybe String -> String -> Int -> (Result Http.Error ManageResult -> msg) -> Cmd msg
deleteCollection apiBase credential collectionId expectedVersion toMsg =
    Http.request
        { method = "DELETE"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "collections", collectionId ]
        , body = Http.jsonBody (Encode.object [ ( "expected_version", Encode.int expectedVersion ) ])
        , expect = Http.expectJson toMsg manageDecoder
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
