module Api.Moderation exposing (listActions, listQueue, moderate, previewPath, queueEndpoint)

import Api.Access exposing (authHeaders)
import Domain.Moderation as Mod exposing (Action, AuditEntry, ModerationResult, QueuePage)
import Http
import Json.Encode as Encode
import Url.Builder


queueEndpoint : String -> Mod.State -> Maybe String -> Int -> String
queueEndpoint apiBase state cursor limit =
    let
        params =
            [ Url.Builder.string "state" (Mod.stateToString state), Url.Builder.int "limit" limit ]
                ++ (case cursor of
                        Just value ->
                            [ Url.Builder.string "cursor" value ]

                        Nothing ->
                            []
                   )
    in
    endpoint apiBase [ "api", "posts", "moderation" ] params


listQueue : String -> Maybe String -> Mod.State -> Maybe String -> Int -> (Result Http.Error QueuePage -> msg) -> Cmd msg
listQueue apiBase credential state cursor limit toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , url = queueEndpoint apiBase state cursor limit
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg Mod.pageDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }


moderate : String -> Maybe String -> List String -> Action -> String -> (Result Http.Error (List ModerationResult) -> msg) -> Cmd msg
moderate apiBase credential ids action reason toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "posts", "moderation" ] []
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "posts", Encode.list (\id -> Encode.object [ ( "id", Encode.string id ) ]) ids )
                    , ( "action", Encode.string (Mod.actionToString action) )
                    , ( "reason", Encode.string reason )
                    ]
                )
        , expect = Http.expectJson toMsg Mod.responseDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }


listActions : String -> Maybe String -> String -> (Result Http.Error (List AuditEntry) -> msg) -> Cmd msg
listActions apiBase credential postId toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "posts", postId, "moderation" ] []
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg Mod.actionsDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }


{-| Path (never a full URL with credentials) of the protected preview.
The JS bridge builds the fetch from this path plus the API base.
-}
previewPath : String -> String
previewPath postId =
    "/api/posts/" ++ postId ++ "/moderation/preview"


endpoint : String -> List String -> List Url.Builder.QueryParameter -> String
endpoint apiBase path params =
    if String.trim apiBase == "" then
        Url.Builder.absolute path params

    else
        Url.Builder.crossOrigin apiBase path params
