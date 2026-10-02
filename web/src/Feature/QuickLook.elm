module Feature.QuickLook exposing (view)

import Api.Post
import Domain.Post exposing (PostSummary)
import Html exposing (Html, a, button, div, img, p, text)
import Html.Attributes exposing (alt, attribute, class, href, src, target, type_)
import Html.Events exposing (onClick)


view : String -> PostSummary -> Int -> Int -> msg -> msg -> msg -> Html msg
view apiBase post index total close previous next =
    div [ class "quicklook-backdrop", attribute "role" "presentation" ]
        [ div [ class "quicklook-dialog", attribute "role" "dialog", attribute "aria-modal" "true", attribute "aria-label" ("Post " ++ post.id) ]
            [ div [ class "quicklook-toolbar" ]
                [ p [ class "quicklook-position" ] [ text (String.fromInt (index + 1) ++ " / " ++ String.fromInt total) ]
                , button [ class "button button-subtle", type_ "button", onClick close ] [ text "Close" ]
                ]
            , img [ class "quicklook-image", src (Api.Post.mediaUrl apiBase post.originalUrl), alt ("Original for post " ++ post.id) ] []
            , div [ class "quicklook-actions" ]
                [ button [ class "button", type_ "button", onClick previous ] [ text "Previous" ]
                , a [ class "button button-subtle", href (Api.Post.mediaUrl apiBase post.originalUrl), target "_blank" ] [ text "Open original" ]
                , button [ class "button", type_ "button", onClick next ] [ text "Next" ]
                ]
            ]
        ]
