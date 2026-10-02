module Ui.Media exposing (image)

import Html exposing (Attribute, Html, div, img, span, text)
import Html.Attributes exposing (alt, attribute, class, src)
import Html.Events exposing (on)
import Json.Decode as Decode


{-| An image that falls back to a same-size "media unavailable" placeholder.
-}
image :
    List (Attribute msg)
    ->
        { url : String
        , postId : String
        , missing : Bool
        , onError : String -> msg
        }
    -> Html msg
image attributes options =
    if options.missing then
        div (class "media-missing" :: attributes)
            [ span [ class "media-missing-id" ] [ text ("#" ++ options.postId) ]
            , span [] [ text "media unavailable" ]
            ]

    else
        img
            ([ src options.url
             , alt ("Post " ++ options.postId)
             , attribute "draggable" "false"
             , on "error" (Decode.succeed (options.onError options.postId))
             ]
                ++ attributes
            )
            []
