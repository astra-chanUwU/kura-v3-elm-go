module Feature.Navigator exposing (Config, view)

import Html exposing (Html, aside, button, div, li, p, span, text, ul)
import Html.Attributes exposing (attribute, class, classList, disabled, title, type_)
import Html.Events exposing (onClick)
import Ui.Panel as Panel exposing (Presentation)


type alias Config msg =
    { presentation : Presentation
    , current : String
    , saved : List String
    , recent : List String
    , onRun : String -> msg
    , onSave : msg
    , onRemove : String -> msg
    , onClose : msg
    }


view : Config msg -> Html msg
view config =
    let
        canSave =
            String.trim config.current /= "" && not (List.member config.current config.saved)
    in
    aside [ class (Panel.presentationClass "navigator" config.presentation), attribute "aria-label" "Navigator" ]
        [ Panel.header "Library" config.presentation config.onClose
        , div [ class "panel-body" ]
            [ Panel.section "Saved searches"
                [ if List.isEmpty config.saved then
                    p [ class "panel-note" ] [ text "Save a search to keep it here." ]

                  else
                    ul [ class "nav-list" ]
                        (List.map
                            (\query ->
                                li [ class "nav-row" ]
                                    [ queryButton config query
                                    , button
                                        [ class "nav-remove"
                                        , type_ "button"
                                        , onClick (config.onRemove query)
                                        , attribute "aria-label" ("Remove saved search " ++ query)
                                        ]
                                        [ text "×" ]
                                    ]
                            )
                            config.saved
                        )
                , button [ class "button button-quiet nav-save", type_ "button", onClick config.onSave, disabled (not canSave) ]
                    [ text "Save current search" ]
                ]
            , Panel.section "Recent"
                [ if List.isEmpty config.recent then
                    p [ class "panel-note" ] [ text "Searches from this session appear here." ]

                  else
                    ul [ class "nav-list" ] (List.map (\query -> li [ class "nav-row" ] [ queryButton config query ]) config.recent)
                ]
            , Panel.section "Collections"
                [ p [ class "panel-note" ] [ text "Collections and pools arrive with the collections API." ] ]
            ]
        ]


queryButton : Config msg -> String -> Html msg
queryButton config query =
    button
        [ class "nav-item"
        , classList [ ( "is-current", query == config.current ) ]
        , type_ "button"
        , title query
        , onClick (config.onRun query)
        ]
        [ span [] [ text query ] ]
