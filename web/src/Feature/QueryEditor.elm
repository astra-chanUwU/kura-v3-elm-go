module Feature.QueryEditor exposing (FieldConfig, FilterConfig, field, fieldId, filterBar)

import App.Prefs exposing (CellExtras)
import Domain.Query
import Html exposing (Html, button, div, form, input, label, li, span, text, ul)
import Html.Attributes as Attr exposing (attribute, class, id, placeholder, type_, value)
import Html.Events exposing (onClick, onInput, onSubmit)
import Ui.Kbd


type alias FieldConfig msg =
    { draft : String
    , onInput : String -> msg
    , onSubmit : msg
    }


fieldId : String
fieldId =
    "query-input"


field : FieldConfig msg -> Html msg
field config =
    form [ class "query-form", onSubmit config.onSubmit, attribute "role" "search" ]
        [ input
            [ id fieldId
            , class "query-input"
            , type_ "search"
            , placeholder "KuraQL — e.g. demo -kson"
            , value config.draft
            , onInput config.onInput
            , attribute "aria-label" "Search posts"
            , attribute "autocomplete" "off"
            , attribute "spellcheck" "false"
            ]
            []
        , span [ class "query-hint" ] [ Ui.Kbd.view "/" ]
        ]


type alias FilterConfig msg =
    { query : String
    , thumb : Int
    , extras : CellExtras
    , onRemoveTerm : Int -> msg
    , onThumb : Int -> msg
    , onCycleExtras : msg
    }


filterBar : FilterConfig msg -> Html msg
filterBar config =
    let
        terms =
            Domain.Query.terms config.query
    in
    div [ class "filterbar" ]
        [ if List.isEmpty terms then
            span [ class "filterbar-empty" ] [ text "No filter" ]

          else
            ul [ class "filterbar-terms", attribute "aria-label" "Query terms" ]
                (List.indexedMap
                    (\index term ->
                        li []
                            [ span [ class "chip chip-term" ]
                                [ span [] [ text term ]
                                , button
                                    [ class "chip-remove"
                                    , type_ "button"
                                    , onClick (config.onRemoveTerm index)
                                    , attribute "aria-label" ("Remove " ++ term)
                                    ]
                                    [ text "×" ]
                                ]
                            ]
                    )
                    terms
                )
        , div [ class "filterbar-controls" ]
            [ span [ class "filterbar-sort" ] [ text "Newest first" ]
            , label [ class "filterbar-size" ]
                [ span [] [ text "Size" ]
                , input
                    [ type_ "range"
                    , Attr.min (String.fromInt App.Prefs.thumbMin)
                    , Attr.max (String.fromInt App.Prefs.thumbMax)
                    , Attr.step "8"
                    , value (String.fromInt config.thumb)
                    , onInput (String.toInt >> Maybe.withDefault config.thumb >> config.onThumb)
                    , attribute "aria-label" "Thumbnail size"
                    ]
                    []
                , Ui.Kbd.view "−"
                , Ui.Kbd.view "="
                ]
            , button [ class "button button-quiet", type_ "button", onClick config.onCycleExtras ]
                [ span [] [ text ("Badges: " ++ App.Prefs.extrasLabel config.extras) ]
                , Ui.Kbd.view "J"
                ]
            ]
        ]
