module Feature.Filmstrip exposing (Config, elementId, itemId, view)

import Api.Post
import App.Keyboard as Keyboard exposing (Modifiers)
import Domain.Selection as Selection exposing (Selection)
import Domain.Sequence as Sequence exposing (Sequence)
import Html exposing (Html, div)
import Html.Attributes exposing (attribute, class, classList, id, tabindex)
import Html.Events exposing (on)
import Html.Keyed as Keyed
import Json.Decode as Decode
import Set exposing (Set)
import Ui.Media


type alias Config msg =
    { apiBase : String
    , sequence : Sequence
    , selection : Selection
    , missing : Set String
    , onClick : String -> Modifiers -> msg
    , onMediaError : String -> msg
    }


elementId : String
elementId =
    "filmstrip"


itemId : String -> String
itemId postId =
    "film-" ++ postId


{-| Renders a stable 60-item window around the active post; the window moves in
chunks of 20 so items do not shift on every step.
-}
view : Config msg -> Html msg
view config =
    let
        activeIndex =
            config.selection.active
                |> Maybe.andThen (\postId -> Sequence.indexOf postId config.sequence)
                |> Maybe.withDefault 0

        start =
            max 0 ((activeIndex // 20 - 1) * 20)
    in
    Keyed.node "div"
        [ id elementId
        , class "filmstrip"
        , tabindex 0
        , attribute "role" "listbox"
        , attribute "aria-multiselectable" "true"
        , attribute "aria-label" "Result sequence"
        ]
        (Sequence.slice start (start + 60) config.sequence
            |> List.map
                (\( _, post ) ->
                    let
                        isSelected =
                            Selection.isSelected post.id config.selection
                    in
                    ( post.id
                    , div
                        [ id (itemId post.id)
                        , class "film-item"
                        , classList
                            [ ( "is-selected", isSelected )
                            , ( "is-active", config.selection.active == Just post.id )
                            ]
                        , attribute "role" "option"
                        , attribute "aria-selected"
                            (if isSelected then
                                "true"

                             else
                                "false"
                            )
                        , attribute "aria-label" ("Post " ++ post.id)
                        , on "click" (Decode.map (config.onClick post.id) Keyboard.modifiersDecoder)
                        ]
                        [ Ui.Media.image [ class "film-image", attribute "loading" "lazy" ]
                            { url = Api.Post.mediaUrl config.apiBase post.previewUrl
                            , postId = post.id
                            , missing = Set.member post.id config.missing
                            , onError = config.onMediaError
                            }
                        ]
                    )
                )
        )
