port module Api.ModerationPreview exposing
    ( PreviewEvent
    , eventDecoder
    , previewEvent
    , releaseAll
    , releaseGeneration
    , request
    )

{-| Authenticated preview bridge contract.

Elm never puts a bearer token in a URL and never receives a public media
URL for a hidden post. It asks the JS bridge (`web/moderation-preview.js`)
to fetch `GET /api/posts/{id}/moderation/preview` with the credential in
an `Authorization` header, and the bridge returns a transient object URL
keyed by (generation, requestId). Object URLs are revoked by the bridge
on release, on a generation change, and when the feature closes, changes
state, or locks. If the bridge or endpoint is unavailable the queue
simply stays metadata-only.
-}

import Json.Decode as Decode exposing (Decoder)
import Json.Encode as Encode


port moderationPreviewRequested : Encode.Value -> Cmd msg


port moderationPreviewReleased : Encode.Value -> Cmd msg


port moderationPreviewEvent : (Encode.Value -> msg) -> Sub msg


type alias PreviewEvent =
    { generation : Int
    , requestId : Int
    , postId : String
    , url : Maybe String
    , error : Maybe String
    }


eventDecoder : Decoder PreviewEvent
eventDecoder =
    Decode.map5 PreviewEvent
        (Decode.field "generation" Decode.int)
        (Decode.field "requestId" Decode.int)
        (Decode.field "postId" Decode.string)
        (Decode.field "url" (Decode.nullable Decode.string))
        (Decode.field "error" (Decode.nullable Decode.string))


{-| Ask the bridge for one post's thumbnail. `credential` travels only in
this transient port value so the bridge can set the Authorization header;
it is never appended to a URL, stored, or logged by the bridge.
-}
request : String -> Maybe String -> Int -> Int -> String -> Cmd msg
request apiBase credential generation requestId postId =
    moderationPreviewRequested
        (Encode.object
            [ ( "generation", Encode.int generation )
            , ( "requestId", Encode.int requestId )
            , ( "postId", Encode.string postId )
            , ( "apiBase", Encode.string apiBase )
            , ( "credential", Maybe.map Encode.string credential |> Maybe.withDefault Encode.null )
            ]
        )


releaseGeneration : Int -> Cmd msg
releaseGeneration generation =
    moderationPreviewReleased (Encode.object [ ( "generation", Encode.int generation ) ])


releaseAll : Cmd msg
releaseAll =
    moderationPreviewReleased (Encode.object [ ( "all", Encode.bool True ) ])


previewEvent : (Encode.Value -> msg) -> Sub msg
previewEvent toMsg =
    moderationPreviewEvent toMsg
