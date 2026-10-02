module Feature.Survey exposing (Config, columns, view)

import Api.Post
import Domain.Post exposing (PostSummary)
import Html exposing (Html, button, div, p, text)
import Html.Attributes exposing (attribute, class, classList, style, type_)
import Html.Events exposing (onClick, stopPropagationOn)
import Html.Keyed as Keyed
import Json.Decode as Decode
import Set exposing (Set)
import Ui.Button
import Ui.Media
import Ui.Toolbar


type alias Config msg =
    { apiBase : String
    , posts : List PostSummary
    , activeId : Maybe String
    , missing : Set String
    , narrow : Bool
    , note : Maybe String
    , onActivate : String -> msg
    , onRemove : String -> msg
    , onClose : msg
    , onMediaError : String -> msg
    }


columns : Bool -> Int -> Int
columns narrow count =
    if narrow then
        2

    else if count <= 3 then
        max 1 count

    else if count == 4 then
        2

    else
        3


view : Config msg -> Html msg
view config =
    let
        count =
            List.length config.posts
    in
    div [ class "survey" ]
        [ Ui.Toolbar.view
            [ p [ class "view-position" ] [ text ("Survey · " ++ String.fromInt count) ]
            , case config.note of
                Just note ->
                    p [ class "view-caption" ] [ text note ]

                Nothing ->
                    text ""
            ]
            [ Ui.Button.view [] { label = "Grid", key = Just "Esc", onPress = Just config.onClose, pressed = Nothing, hint = Nothing } ]
        , Keyed.node "div"
            [ class "survey-tiles"
            , style "grid-template-columns" ("repeat(" ++ String.fromInt (columns config.narrow count) ++ ", minmax(0, 1fr))")
            ]
            (List.map (\post -> ( post.id, tile config post )) config.posts)
        ]


tile : Config msg -> PostSummary -> Html msg
tile config post =
    div
        [ class "survey-tile"
        , classList [ ( "is-active", config.activeId == Just post.id ) ]
        , onClick (config.onActivate post.id)
        ]
        [ Ui.Media.image [ class "survey-image" ]
            { url = Api.Post.mediaUrl config.apiBase post.originalUrl
            , postId = post.id
            , missing = Set.member post.id config.missing
            , onError = config.onMediaError
            }
        , p [ class "survey-caption" ] [ text ("#" ++ post.id ++ " · " ++ String.fromInt post.width ++ " × " ++ String.fromInt post.height) ]
        , button
            [ class "survey-remove"
            , type_ "button"
            , attribute "aria-label" ("Remove post " ++ post.id ++ " from survey")
            , stopPropagationOn "click" (Decode.succeed ( config.onRemove post.id, True ))
            ]
            [ text "×" ]
        ]
