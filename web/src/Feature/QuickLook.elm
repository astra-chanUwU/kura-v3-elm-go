module Feature.QuickLook exposing (Config, Zoom(..), toggleZoom, view)

import Api.Post
import Domain.Post exposing (PostSummary)
import Html exposing (Html, button, div, p, span, text)
import Html.Attributes exposing (attribute, class, classList, disabled, title, type_)
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
            [ Ui.Button.view [ class "button-quiet" ] { label = "← Back to grid", key = Nothing, onPress = Just config.onClose, pressed = Nothing, hint = Just "Back to grid (Esc)" }
            , p [ class "view-caption" ]
                [ span [ class "view-caption-id" ] [ text ("#" ++ post.id) ]
                , text (" · " ++ String.fromInt post.width ++ " × " ++ String.fromInt post.height)
                ]
            ]
            [ Ui.Button.view []
                { label = "Fit"
                , key = Nothing
                , onPress =
                    if config.zoom == Actual then
                        Just config.onZoom

                    else
                        Nothing
                , pressed = Just (config.zoom == Fit)
                , hint = Just "Toggle fit / actual size (Z)"
                }
            , Ui.Button.view []
                { label = "100%"
                , key = Nothing
                , onPress =
                    if config.zoom == Fit then
                        Just config.onZoom

                    else
                        Nothing
                , pressed = Just (config.zoom == Actual)
                , hint = Just "Toggle fit / actual size (Z)"
                }
            ]
        , div [ class "loupe-canvas", classList [ ( "is-actual", config.zoom == Actual ) ] ]
            [ Ui.Media.image
                [ class "loupe-image", onClick config.onZoom ]
                { url = originalUrl, postId = post.id, missing = config.missing, onError = config.onMediaError }
            , navArrow "previous" "Previous image" "‹" (previousIf config)
            , navArrow "next" "Next image" "›" (nextIf config)
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


navArrow : String -> String -> String -> Maybe msg -> Html msg
navArrow direction label glyph action =
    button
        ([ class ("loupe-arrow loupe-arrow-" ++ direction), type_ "button", attribute "aria-label" label, title label ]
            ++ (case action of
                    Just msg ->
                        [ onClick msg ]

                    Nothing ->
                        [ disabled True ]
               )
        )
        [ text glyph ]
