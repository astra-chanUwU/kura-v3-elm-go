module Feature.MediaGrid exposing (Config, elementId, cellId, view)

import Api.Post
import App.Keyboard as Keyboard exposing (Modifiers)
import App.Prefs exposing (CellExtras(..))
import Domain.Post exposing (PostSummary)
import Domain.Selection as Selection exposing (Selection)
import Domain.Sequence as Sequence exposing (Sequence)
import Feature.MediaGrid.Layout as Layout exposing (Geometry, Viewport)
import Html exposing (Html, button, div, img, span, text)
import Html.Attributes exposing (alt, attribute, class, classList, id, src, style, tabindex, type_)
import Html.Events exposing (on, preventDefaultOn, stopPropagationOn)
import Html.Keyed as Keyed
import Json.Decode as Decode
import Set exposing (Set)


type alias Config msg =
    { apiBase : String
    , sequence : Sequence
    , selection : Selection
    , thumb : Int
    , extras : CellExtras
    , viewport : Viewport
    , missing : Set String
    , hidden : Bool
    , stale : Bool
    , onCell : String -> Modifiers -> msg
    , onOpen : String -> msg
    , onCheck : String -> msg
    , onScroll : Viewport -> msg
    , onMediaError : String -> msg
    , noop : msg
    }


elementId : String
elementId =
    "media-grid-viewport"


cellId : String -> String
cellId postId =
    "cell-" ++ postId


view : Config msg -> Html msg
view config =
    let
        g =
            Layout.geometry config.viewport.width config.thumb

        count =
            Sequence.length config.sequence

        ( from, to ) =
            Layout.visibleRange g config.viewport count 3

        activeIndex =
            config.selection.active |> Maybe.andThen (\postId -> Sequence.indexOf postId config.sequence)

        tabStop =
            case activeIndex of
                Just index ->
                    index

                Nothing ->
                    from

        visible =
            Sequence.slice from to config.sequence

        cells =
            case activeIndex of
                Just index ->
                    if index < from || index >= to then
                        Sequence.slice index (index + 1) config.sequence ++ visible

                    else
                        visible

                Nothing ->
                    visible
    in
    div
        ([ id elementId
         , class "media-grid-viewport"
         , class ("extras-" ++ App.Prefs.extrasLabel config.extras)
         , classList [ ( "is-hidden", config.hidden ), ( "is-stale", config.stale ) ]
         , attribute "role" "listbox"
         , attribute "aria-multiselectable" "true"
         , attribute "aria-label" "Search results"
         , on "scroll" (Decode.map config.onScroll scrollDecoder)
         ]
            ++ (if config.hidden then
                    [ attribute "inert" "" ]

                else
                    []
               )
        )
        [ Keyed.node "div"
            [ class "media-grid", style "height" (px (Layout.totalHeight g count)) ]
            (List.map (\( index, post ) -> ( post.id, cell config g tabStop index post )) cells)
        ]


cell : Config msg -> Geometry -> Int -> Int -> PostSummary -> Html msg
cell config g tabStop index post =
    let
        isSelected =
            Selection.isSelected post.id config.selection

        isActive =
            config.selection.active == Just post.id

        isMissing =
            Set.member post.id config.missing

        dims =
            String.fromInt post.width ++ " × " ++ String.fromInt post.height
    in
    div
        [ id (cellId post.id)
        , class "cell"
        , classList [ ( "is-selected", isSelected ), ( "is-active", isActive ), ( "is-missing", isMissing ) ]
        , attribute "role" "option"
        , attribute "aria-selected" (boolString isSelected)
        , attribute "aria-label" ("Post " ++ post.id ++ ", " ++ dims)
        , tabindex
            (if index == tabStop then
                0

             else
                -1
            )
        , style "left" (px (Layout.left g index))
        , style "top" (px (Layout.cellTop g index))
        , style "width" (px g.cell)
        , style "height" (px g.cell)
        , on "click" (Decode.map (config.onCell post.id) Keyboard.modifiersDecoder)
        , on "dblclick" (Decode.succeed (config.onOpen post.id))
        ]
        [ if isMissing then
            div [ class "cell-missing" ]
                [ span [ class "cell-missing-id" ] [ text ("#" ++ post.id) ]
                , span [] [ text "media unavailable" ]
                ]

          else
            img
                [ class "cell-image"
                , src (Api.Post.mediaUrl config.apiBase post.previewUrl)
                , alt ""
                , attribute "loading" "lazy"
                , attribute "decoding" "async"
                , attribute "draggable" "false"
                , on "error" (Decode.succeed (config.onMediaError post.id))
                ]
                []
        , button
            [ class "cell-check"
            , type_ "button"
            , tabindex -1
            , attribute "aria-pressed" (boolString isSelected)
            , attribute "aria-label" ("Select post " ++ post.id)
            , stopPropagationOn "click" (Decode.succeed ( config.onCheck post.id, True ))
            , stopPropagationOn "dblclick" (Decode.succeed ( config.noop, True ))
            , preventDefaultOn "mousedown" (Decode.succeed ( config.noop, True ))
            ]
            [ text "✓" ]
        , span [ class "cell-badge" ] [ text ("#" ++ post.id ++ " · " ++ dims) ]
        , if config.extras == ExtrasAlways then
            div [ class "cell-footer" ]
                [ span [] [ text ("#" ++ post.id) ]
                , span [] [ text (dims ++ " · " ++ post.mediaType) ]
                ]

          else
            text ""
        ]


scrollDecoder : Decode.Decoder Viewport
scrollDecoder =
    Decode.map3 Viewport
        (Decode.at [ "target", "scrollTop" ] Decode.float)
        (Decode.at [ "target", "clientWidth" ] Decode.float)
        (Decode.at [ "target", "clientHeight" ] Decode.float)


px : Float -> String
px value =
    String.fromFloat value ++ "px"


boolString : Bool -> String
boolString value =
    if value then
        "true"

    else
        "false"
