module Feature.QueryEditor exposing (FieldConfig, FilterConfig, field, fieldId, filterBar)

import App.Prefs exposing (CellExtras)
import Domain.Query
import Html exposing (Html, button, div, form, input, label, li, option, select, span, text, ul)
import Html.Attributes as Attr exposing (attribute, class, id, placeholder, type_, value)
import Html.Events exposing (onClick, onInput, onSubmit)
import Ui.Button
import Ui.Icon
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
        [ Ui.Icon.view "search"
        , input
            [ id fieldId
            , class "query-input"
            , type_ "search"
            , placeholder "Search your library"
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
    , onOrder : String -> msg
    , collectionName : Maybe String
    }


filterBar : FilterConfig msg -> Html msg
filterBar config =
    let
        terms =
            Domain.Query.terms config.query
                |> List.indexedMap Tuple.pair
                |> List.filter (Tuple.second >> String.startsWith "order:" >> not)
    in
    div [ class "filterbar" ]
        [ if List.isEmpty terms then
            span [ class "filterbar-empty" ] [ text (Maybe.withDefault "All posts" config.collectionName) ]

          else
            ul [ class "filterbar-terms", attribute "aria-label" "Query terms" ]
                (List.map
                    (\( index, term ) ->
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
            [ case config.collectionName of
                Just _ ->
                    span [ class "filterbar-sort" ] [ text "Collection order" ]

                Nothing ->
                    select [ class "sort-select", value (Domain.Query.order config.query), onInput config.onOrder, attribute "aria-label" "Sort posts" ]
                        [ option [ value "newest", Attr.selected (Domain.Query.order config.query == "newest") ] [ text "Newest" ]
                        , option [ value "score", Attr.selected (Domain.Query.order config.query == "score") ] [ text "Highest score" ]
                        ]
            , label [ class "filterbar-size" ]
                [ Ui.Icon.view "grid"
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
                ]
            , Ui.Button.icon "badges"
                [ class "button-quiet" ]
                { label = "Thumbnail badges: " ++ App.Prefs.extrasLabel config.extras
                , key = Nothing
                , onPress = Just config.onCycleExtras
                , pressed = Just (config.extras /= App.Prefs.ExtrasOff)
                , hint = Just "Cycle thumbnail badges (J)"
                }
            ]
        ]
