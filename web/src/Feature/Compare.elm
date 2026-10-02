module Feature.Compare exposing (Config, view)

import Api.Post
import Domain.Post exposing (PostSummary)
import Feature.QuickLook exposing (Zoom(..))
import Html exposing (Html, dd, div, dl, dt, p, span, text)
import Html.Attributes exposing (class, classList)
import Html.Events exposing (onClick)
import Set exposing (Set)
import Ui.Button
import Ui.Media
import Ui.Toolbar


type alias Config msg =
    { apiBase : String
    , select : PostSummary
    , candidate : PostSummary
    , activeId : Maybe String
    , zoom : Zoom
    , missing : Set String
    , onPrevious : msg
    , onNext : msg
    , onSwap : msg
    , onPromote : msg
    , onZoom : msg
    , onClose : msg
    , onActivate : String -> msg
    , onMediaError : String -> msg
    }


view : Config msg -> Html msg
view config =
    div [ class "compare" ]
        [ Ui.Toolbar.view
            [ p [ class "view-position" ] [ text "Compare" ] ]
            [ Ui.Button.view [] { label = "Candidate", key = Just "←", onPress = Just config.onPrevious, pressed = Nothing, hint = Just "Previous candidate" }
            , Ui.Button.view [] { label = "Candidate", key = Just "→", onPress = Just config.onNext, pressed = Nothing, hint = Just "Next candidate" }
            , Ui.Button.view [] { label = "Swap", key = Just "↓", onPress = Just config.onSwap, pressed = Nothing, hint = Nothing }
            , Ui.Button.view [] { label = "Make select", key = Just "↑", onPress = Just config.onPromote, pressed = Nothing, hint = Just "Candidate becomes the select" }
            , Ui.Button.view []
                { label =
                    if config.zoom == Fit then
                        "Fit"

                    else
                        "1:1"
                , key = Just "Z"
                , onPress = Just config.onZoom
                , pressed = Just (config.zoom == Actual)
                , hint = Nothing
                }
            , Ui.Button.view [] { label = "Grid", key = Just "Esc", onPress = Just config.onClose, pressed = Nothing, hint = Nothing }
            ]
        , div [ class "compare-panes" ]
            [ pane config "Select" config.select config.candidate
            , pane config "Candidate" config.candidate config.select
            ]
        ]


pane : Config msg -> String -> PostSummary -> PostSummary -> Html msg
pane config label post other =
    div
        [ class "compare-pane"
        , classList [ ( "is-active", config.activeId == Just post.id ) ]
        , onClick (config.onActivate post.id)
        ]
        [ div [ class "compare-canvas", classList [ ( "is-actual", config.zoom == Actual ) ] ]
            [ Ui.Media.image [ class "compare-image" ]
                { url = Api.Post.mediaUrl config.apiBase post.originalUrl
                , postId = post.id
                , missing = Set.member post.id config.missing
                , onError = config.onMediaError
                }
            ]
        , dl [ class "compare-meta" ]
            [ row label ("#" ++ post.id) False
            , row "Dimensions" (dimensions post) (dimensions post /= dimensions other)
            , row "Pixels" (megapixels post) (post.width * post.height /= other.width * other.height)
            , row "Media type" post.mediaType (post.mediaType /= other.mediaType)
            ]
        ]


row : String -> String -> Bool -> Html msg
row label value different =
    div [ class "compare-meta-row", classList [ ( "is-different", different ) ] ]
        [ dt [] [ text label ]
        , dd [] [ span [] [ text value ] ]
        ]


dimensions : PostSummary -> String
dimensions post =
    String.fromInt post.width ++ " × " ++ String.fromInt post.height


megapixels : PostSummary -> String
megapixels post =
    let
        tenths =
            round (toFloat (post.width * post.height) / 100000)
    in
    String.fromInt (tenths // 10) ++ "." ++ String.fromInt (modBy 10 tenths) ++ " MP"
