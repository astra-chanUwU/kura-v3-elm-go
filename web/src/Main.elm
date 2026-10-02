module Main exposing (main)

import Browser
import Html exposing (Html, div, h1, header, input, main_, nav, section, text)
import Html.Attributes exposing (class, placeholder, value)
import Html.Events exposing (onInput)


type alias Model =
    { query : String
    }


type Msg
    = QueryChanged String


main : Program () Model Msg
main =
    Browser.element
        { init = \_ -> ( { query = "" }, Cmd.none )
        , update = update
        , subscriptions = \_ -> Sub.none
        , view = view
        }


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        QueryChanged query ->
            ( { model | query = query }, Cmd.none )


view : Model -> Html Msg
view model =
    div [ class "kura-shell" ]
        [ header [ class "kura-header" ]
            [ h1 [] [ text "Kura V3" ]
            , nav [] [ text "Media" ]
            ]
        , main_ [ class "kura-main" ]
            [ section [ class "search-panel" ]
                [ input
                    [ class "search-input"
                    , placeholder "Search posts"
                    , value model.query
                    , onInput QueryChanged
                    ]
                    []
                ]
            , section [ class "media-grid" ]
                [ text "MediaGrid will render post summaries here." ]
            ]
        ]
