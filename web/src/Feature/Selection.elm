module Feature.Selection exposing (Config, view)

import Html exposing (Html, div, p, text)
import Html.Attributes exposing (class)
import Ui.Button


type alias Config msg =
    { count : Int
    , activeId : Maybe String
    , onClear : msg
    , onApiPending : String -> msg
    , onFavorite : msg
    , onAddToCollection : msg
    , writesEnabled : Bool
    }


{-| Actions apply to the selection when it is non-empty, otherwise to the
active post. -}
view : Config msg -> Html msg
view config =
    let
        target =
            if config.count > 0 then
                Just (String.fromInt config.count ++ " selected")

            else
                Maybe.map (\postId -> "#" ++ postId) config.activeId
    in
    case target of
        Nothing ->
            p [ class "panel-note" ] [ text "Nothing to act on yet." ]

        Just label ->
            div [ class "selection-actions" ]
                [ p [ class "selection-target" ] [ text ("Actions apply to " ++ label) ]
                , div [ class "selection-buttons" ]
                    [ pending config "Tag" "T" "Tag editing"
                    , Ui.Button.view []
                        { label = "Add to collection"
                        , key = Just "B"
                        , onPress =
                            if config.writesEnabled then
                                Just config.onAddToCollection

                            else
                                Nothing
                        , pressed = Nothing
                        , hint =
                            if config.writesEnabled then
                                Just "Add selected posts to the active collection"

                            else
                                Just "Unlock to add to a collection"
                        }
                    , Ui.Button.view []
                        { label = "Favorite"
                        , key = Just "F"
                        , onPress =
                            if config.writesEnabled then
                                Just config.onFavorite

                            else
                                Nothing
                        , pressed = Nothing
                        , hint =
                            if config.writesEnabled then
                                Just "Toggle favorite"

                            else
                                Just "Unlock to change favorites"
                        }
                    , Ui.Button.view []
                        { label = "Clear selection"
                        , key = Just "Ctrl D"
                        , onPress =
                            if config.count > 0 then
                                Just config.onClear

                            else
                                Nothing
                        , pressed = Nothing
                        , hint = Nothing
                        }
                    ]
                ]


pending : Config msg -> String -> String -> String -> Html msg
pending config label key feature =
    Ui.Button.view [ class "is-pending" ]
        { label = label
        , key = Just key
        , onPress = Just (config.onApiPending feature)
        , pressed = Nothing
        , hint = Just (feature ++ " needs a server API that does not exist yet")
        }
