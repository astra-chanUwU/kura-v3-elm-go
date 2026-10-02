module Feature.MediaGrid exposing (view)

import Domain.Post exposing (PostSummary)
import Api.Post
import Html exposing (Html, article, button, div, img, p, span, text)
import Html.Attributes exposing (alt, attribute, class, height, src, type_, width)
import Html.Events exposing (onClick)
import Set exposing (Set)


view : String -> List PostSummary -> Set String -> (PostSummary -> msg) -> (String -> msg) -> Html msg
view apiBase posts selected open toggle =
    div [ class "media-grid" ] (List.map (card apiBase selected open toggle) posts)


card : String -> Set String -> (PostSummary -> msg) -> (String -> msg) -> PostSummary -> Html msg
card apiBase selected open toggle post =
    let
        isSelected = Set.member post.id selected
        ratio =
            if post.height > 0 then
                String.fromFloat (toFloat post.width / toFloat post.height)

            else
                "1"
    in
    article [ class ("media-card" ++ if isSelected then " media-card-selected" else ""), attribute "style" ("--media-ratio:" ++ ratio) ]
        [ button [ class "media-open", type_ "button", onClick (open post) ]
            [ img
                [ class "media-preview"
                , src (Api.Post.mediaUrl apiBase post.previewUrl)
                , alt ("Post " ++ post.id)
                , width post.width
                , height post.height
                ]
                []
            ]
        , div [ class "media-card-footer" ]
            [ button [ class "selection-toggle", type_ "button", onClick (toggle post.id), attribute "aria-pressed" (if isSelected then "true" else "false") ]
                [ span [ class "selection-mark" ] [ text (if isSelected then "✓" else "＋") ]
                , span [] [ text (if isSelected then "Selected" else "Select") ]
                ]
            , p [ class "media-meta" ] [ text (post.mediaType ++ " · " ++ String.fromInt post.width ++ "×" ++ String.fromInt post.height) ]
            ]
        ]
