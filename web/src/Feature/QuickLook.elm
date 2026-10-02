module Feature.QuickLook exposing (Config, Zoom(..), toggleZoom, view)

import Api.Post
import Domain.Post exposing (PostSummary)
import Html exposing (Html, a, div, p, span, text)
import Html.Attributes exposing (class, classList, href, rel, target)
import Html.Events exposing (onClick)
import Ui.Button
import Ui.Media
import Ui.Toolbar


type Zoom
    = Fit
    | Actual


toggleZoom : Zoom -> Zoom
toggleZoom zoom =
    case zoom of
        Fit ->
            Actual

        Actual ->
            Fit


type alias Config msg =
    { apiBase : String
    , post : PostSummary
    , index : Int
    , total : Int
    , zoom : Zoom
    , missing : Bool
    , onZoom : msg
    , onPrevious : msg
    , onNext : msg
    , onClose : msg
    , onMediaError : String -> msg
    }


view : Config msg -> Html msg
view config =
    let
        post =
            config.post

        originalUrl =
            Api.Post.mediaUrl config.apiBase post.originalUrl
    in
    div [ class "loupe" ]
        [ Ui.Toolbar.view
            [ p [ class "view-position" ] [ text (String.fromInt (config.index + 1) ++ " / " ++ String.fromInt config.total) ]
            , p [ class "view-caption" ]
                [ span [ class "view-caption-id" ] [ text ("#" ++ post.id) ]
                , text (" · " ++ String.fromInt post.width ++ " × " ++ String.fromInt post.height ++ " · " ++ post.mediaType)
                ]
            ]
            [ Ui.Button.view [] { label = "Previous", key = Just "←", onPress = previousIf config, pressed = Nothing, hint = Nothing }
            , Ui.Button.view [] { label = "Next", key = Just "→", onPress = nextIf config, pressed = Nothing, hint = Nothing }
            , Ui.Button.view []
                { label =
                    if config.zoom == Fit then
                        "Fit"

                    else
                        "1:1"
                , key = Just "Z"
                , onPress = Just config.onZoom
                , pressed = Just (config.zoom == Actual)
                , hint = Just "Toggle fit / actual size"
                }
            , a [ class "button", href originalUrl, target "_blank", rel "noopener" ] [ text "Original" ]
            , Ui.Button.view [] { label = "Grid", key = Just "Esc", onPress = Just config.onClose, pressed = Nothing, hint = Nothing }
            ]
        , div [ class "loupe-canvas", classList [ ( "is-actual", config.zoom == Actual ) ] ]
            [ Ui.Media.image
                [ class "loupe-image", onClick config.onZoom ]
                { url = originalUrl, postId = post.id, missing = config.missing, onError = config.onMediaError }
            ]
        ]


previousIf : Config msg -> Maybe msg
previousIf config =
    if config.index > 0 then
        Just config.onPrevious

    else
        Nothing


nextIf : Config msg -> Maybe msg
nextIf config =
    if config.index < config.total - 1 then
        Just config.onNext

    else
        Nothing
